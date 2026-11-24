// Package server runs a WireGuard server whose tunnel ends in a gateway.Gateway.
package server

import (
	"encoding/hex"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"

	"hotspot/core/internal/gateway"
	"hotspot/core/internal/wgkey"
)

// Peer is one client that may use the gateway.
type Peer struct {
	PublicKey    wgkey.Key
	PresharedKey wgkey.Key  // zero means none
	IPv4, IPv6   netip.Addr // the client's tunnel addresses; the zero Addr means unused
}

// Config describes the server.
type Config struct {
	PrivateKey wgkey.Key
	ListenPort uint16
	Peers      []Peer
	Gateway    gateway.Options
	// LogLevel is one of device.LogLevelSilent, LogLevelError, LogLevelVerbose.
	LogLevel int
	// Bind carries the encrypted packets; nil means UDP sockets on ListenPort. Benchmarks pass an in-memory one,
	// so that what they measure is the tunnel and not the test computer's network stack.
	Bind conn.Bind
}

// Server is a running WireGuard gateway.
type Server struct {
	dev *device.Device
	gw  *gateway.Gateway

	mu    sync.Mutex
	peers map[wgkey.Key]Peer // the clients the device knows now
}

func byKey(peers []Peer) map[wgkey.Key]Peer {
	m := make(map[wgkey.Key]Peer, len(peers))
	for _, p := range peers {
		m[p.PublicKey] = p
	}
	return m
}

// uapi renders cfg in wireguard-go's userspace configuration format.
func (c Config) uapi() string {
	return fmt.Sprintf("private_key=%s\nlisten_port=%d\n", c.PrivateKey.Hex(), c.ListenPort) + peersUAPI(c.Peers)
}

// peersUAPI renders a configuration that replaces the whole peer set.
func peersUAPI(peers []Peer) string {
	var b strings.Builder
	b.WriteString("replace_peers=true\n")
	for _, p := range peers {
		peerUAPI(&b, p)
	}
	return b.String()
}

func peerUAPI(b *strings.Builder, p Peer) {
	fmt.Fprintf(b, "public_key=%s\n", p.PublicKey.Hex())
	if !p.PresharedKey.IsZero() {
		fmt.Fprintf(b, "preshared_key=%s\n", p.PresharedKey.Hex())
	}
	if p.IPv4.IsValid() {
		fmt.Fprintf(b, "allowed_ip=%s/32\n", p.IPv4)
	}
	if p.IPv6.IsValid() {
		fmt.Fprintf(b, "allowed_ip=%s/128\n", p.IPv6)
	}
}

// peersDelta renders what turns the peer set old into next and touches nobody else: a peer that left is removed, a
// new one is added, one whose addresses or preshared key changed is set up afresh. The peers that stay keep their
// sessions. (Replacing the whole set would throw every session away: each client's traffic would then stall until
// it notices, some fifteen seconds, whenever a device is added or removed.)
func peersDelta(old map[wgkey.Key]Peer, next []Peer) string {
	var b strings.Builder
	stays := make(map[wgkey.Key]bool, len(next))
	for _, p := range next {
		stays[p.PublicKey] = true
		was, known := old[p.PublicKey]
		if known && was == p {
			continue
		}
		if known {
			fmt.Fprintf(&b, "public_key=%s\nremove=true\n", p.PublicKey.Hex())
		}
		peerUAPI(&b, p)
	}
	var gone []string
	for key := range old {
		if !stays[key] {
			gone = append(gone, key.Hex())
		}
	}
	sort.Strings(gone)
	for _, key := range gone {
		fmt.Fprintf(&b, "public_key=%s\nremove=true\n", key)
	}
	return b.String()
}

// Start brings the server up.
func Start(cfg Config) (*Server, error) {
	gw, err := gateway.New(cfg.Gateway)
	if err != nil {
		return nil, err
	}
	bind := cfg.Bind
	if bind == nil {
		bind = conn.NewDefaultBind()
	}
	dev := device.NewDevice(gw, bind, device.NewLogger(cfg.LogLevel, "wg: "))
	if err := dev.IpcSet(cfg.uapi()); err != nil {
		dev.Close()
		return nil, fmt.Errorf("server: configure: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, fmt.Errorf("server: up: %w", err)
	}
	return &Server{dev: dev, gw: gw, peers: byKey(cfg.Peers)}, nil
}

// Stats returns the gateway's anonymous counters.
func (s *Server) Stats() gateway.Stats { return s.gw.Stats() }

// CutClient ends the flows under way of the client with this tunnel address (see gateway.Options.Paused).
func (s *Server) CutClient(client netip.Addr) int { return s.gw.CutClient(client) }

// ClientBytes returns, per client address, the bytes its flows moved to and from the internet (see
// gateway.Gateway.ClientBytes).
func (s *Server) ClientBytes() map[netip.Addr]int64 { return s.gw.ClientBytes() }

// UpdatePeers makes peers the set of allowed clients while the server runs. Clients that stay are not touched:
// their traffic goes on.
func (s *Server) UpdatePeers(peers []Peer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if delta := peersDelta(s.peers, peers); delta != "" {
		if err := s.dev.IpcSet(delta); err != nil {
			// The device may be half-way: put the whole set, at the price of everyone's session.
			if err := s.dev.IpcSet(peersUAPI(peers)); err != nil {
				return err
			}
		}
	}
	s.peers = byKey(peers)
	return nil
}

// ForgetClient ends the flows of the client with this tunnel address and drops what was counted for it (see
// gateway.Gateway.ForgetClient).
func (s *Server) ForgetClient(client netip.Addr) { s.gw.ForgetClient(client) }

// PeerStat is what the server knows of one peer's traffic.
type PeerStat struct {
	Handshake time.Time // the latest completed handshake; zero if there was none
	RxBytes   int64     // received from the peer since the server started
	TxBytes   int64     // sent to the peer since the server started
}

// PeerStats returns the latest handshake and the bytes received from and sent to every peer. The counters start
// again from zero whenever the peers are set anew (replace_peers).
func (s *Server) PeerStats() map[wgkey.Key]PeerStat {
	text, err := s.dev.IpcGet()
	if err != nil {
		return nil
	}
	out := map[wgkey.Key]PeerStat{}
	var peer wgkey.Key
	var sec, nsec, rx, tx int64
	flush := func() {
		if peer.IsZero() {
			return
		}
		st := PeerStat{RxBytes: rx, TxBytes: tx}
		if sec > 0 {
			st.Handshake = time.Unix(sec, nsec)
		}
		out[peer] = st
	}
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "public_key":
			flush()
			peer, sec, nsec, rx, tx = wgkey.Key{}, 0, 0, 0, 0
			if b, err := hex.DecodeString(value); err == nil && len(b) == len(peer) {
				copy(peer[:], b)
			}
		case "last_handshake_time_sec":
			sec, _ = strconv.ParseInt(value, 10, 64)
		case "last_handshake_time_nsec":
			nsec, _ = strconv.ParseInt(value, 10, 64)
		case "rx_bytes":
			rx, _ = strconv.ParseInt(value, 10, 64)
		case "tx_bytes":
			tx, _ = strconv.ParseInt(value, 10, 64)
		}
	}
	flush()
	return out
}

// Close stops the server and the gateway.
func (s *Server) Close() { s.dev.Close() }
