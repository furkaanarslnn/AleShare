// Package provision manages the set of clients that may use the gateway and
// renders their WireGuard configuration. It knows nothing about storage: the
// caller persists the JSON form of State wherever suits the platform (a file on
// the desktop, encrypted preferences on Android).
//
// A client either gets a ready-made configuration (AddPeer: the private key is
// returned once, shown as a QR code and forgotten) or generates its own key pair
// and only hands over the public key (EnrollPeer).
package provision

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"hotspot/core/internal/server"
	"hotspot/core/internal/wgkey"
)

const (
	TunnelMTU = 1420
	MaxPeers  = 250
)

// Gateway addresses inside the tunnel. Clients use them as DNS servers.
var (
	GatewayV4 = netip.MustParseAddr("10.8.0.1")
	GatewayV6 = netip.MustParseAddr("fd42:42:42::1")
)

// Peer is a registered client.
type Peer struct {
	Name         string     `json:"name"`
	PublicKey    wgkey.Key  `json:"public_key"`
	PresharedKey wgkey.Key  `json:"preshared_key"` // zero when the client chose its own key
	IPv4         netip.Addr `json:"ipv4"`
	IPv6         netip.Addr `json:"ipv6"`
}

// State is everything the server must remember.
type State struct {
	ServerKey wgkey.Key `json:"server_private_key"`
	Peers     []Peer    `json:"peers"`
}

// NewState returns an empty state with a fresh server key.
func NewState() *State { return &State{ServerKey: wgkey.Generate()} }

// ParseState reads the JSON form produced by Marshal.
func ParseState(data []byte) (*State, error) {
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.ServerKey.IsZero() {
		return nil, errors.New("provision: server key missing")
	}
	return &s, nil
}

// Marshal returns the JSON form of the state.
func (s *State) Marshal() ([]byte, error) { return json.MarshalIndent(s, "", "  ") }

// ServerPublicKey is the key clients put in their [Peer] section.
func (s *State) ServerPublicKey() wgkey.Key { return s.ServerKey.Public() }

// slot is the index a peer's addresses were derived from, or -1.
func slot(p Peer) int {
	if !p.IPv4.Is4() {
		return -1
	}
	return int(p.IPv4.As4()[3]) - 2
}

// freeSlot returns the lowest unused address slot.
func (s *State) freeSlot() (int, error) {
	used := make(map[int]bool)
	for _, p := range s.Peers {
		used[slot(p)] = true
	}
	for n := 0; n < MaxPeers; n++ {
		if !used[n] {
			return n, nil
		}
	}
	return 0, errors.New("provision: client limit reached")
}

func addressesFor(n int) (netip.Addr, netip.Addr) {
	return netip.AddrFrom4([4]byte{10, 8, 0, byte(2 + n)}), netip.MustParseAddr(fmt.Sprintf("fd42:42:42::%x", 2+n))
}

func (s *State) hasName(name string) bool {
	for _, p := range s.Peers {
		if p.Name == name {
			return true
		}
	}
	return false
}

// AddPeer registers a client in the lowest free address slot and returns it
// together with its private key, which the caller must show once and forget.
func (s *State) AddPeer(name string) (Peer, wgkey.Key, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Peer{}, wgkey.Key{}, errors.New("provision: empty client name")
	}
	if s.hasName(name) {
		return Peer{}, wgkey.Key{}, fmt.Errorf("provision: client %q already exists", name)
	}
	n, err := s.freeSlot()
	if err != nil {
		return Peer{}, wgkey.Key{}, err
	}
	priv := wgkey.Generate()
	p := Peer{Name: name, PublicKey: priv.Public(), PresharedKey: wgkey.Random()}
	p.IPv4, p.IPv6 = addressesFor(n)
	s.Peers = append(s.Peers, p)
	return p, priv, nil
}

// EnrollPeer registers a client that generated its own key pair: only its public
// key is known here, so nothing secret ever has to be transferred. A client of
// the same name is replaced (a reinstalled computer pairs again under its name).
func (s *State) EnrollPeer(name string, public wgkey.Key) (Peer, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Peer{}, errors.New("provision: empty client name")
	}
	if public.IsZero() {
		return Peer{}, errors.New("provision: empty public key")
	}
	for _, p := range s.Peers {
		if p.PublicKey == public && p.Name != name {
			return Peer{}, fmt.Errorf("provision: that key is already registered as %q", p.Name)
		}
	}
	s.RemovePeer(name)
	n, err := s.freeSlot()
	if err != nil {
		return Peer{}, err
	}
	p := Peer{Name: name, PublicKey: public}
	p.IPv4, p.IPv6 = addressesFor(n)
	s.Peers = append(s.Peers, p)
	return p, nil
}

// RemovePeer revokes a client and reports whether it existed.
func (s *State) RemovePeer(name string) bool {
	for i, p := range s.Peers {
		if p.Name == name {
			s.Peers = append(s.Peers[:i], s.Peers[i+1:]...)
			return true
		}
	}
	return false
}

// ServerPeers converts the registered clients to what the server needs.
func (s *State) ServerPeers() []server.Peer {
	peers := make([]server.Peer, len(s.Peers))
	for i, p := range s.Peers {
		peers[i] = server.Peer{PublicKey: p.PublicKey, PresharedKey: p.PresharedKey, IPv4: p.IPv4, IPv6: p.IPv6}
	}
	return peers
}

// ClientConfig renders a standard WireGuard .conf for a client.
func ClientConfig(priv wgkey.Key, p Peer, serverPub wgkey.Key, endpoint string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\nPrivateKey = %s\nAddress = %s/32, %s/128\nDNS = %s, %s\nMTU = %d\n\n",
		priv.Base64(), p.IPv4, p.IPv6, GatewayV4, GatewayV6, TunnelMTU)
	fmt.Fprintf(&b, "[Peer]\nPublicKey = %s\nPresharedKey = %s\nEndpoint = %s\nAllowedIPs = 0.0.0.0/0, ::/0\n",
		serverPub.Base64(), p.PresharedKey.Base64(), endpoint)
	return b.String()
}
