// Package wgcore is the surface of the Hotspot core that gomobile exposes to
// the Android app (gomobile bind ./wgcore). It sticks to the types gomobile can
// carry: strings, ints, bools, error, pointers to exported structs, and
// interfaces the app implements.
//
// The app owns persistence: it stores StateJSON() (server key and the public
// side of every client) in encrypted preferences and passes it back to NewCore.
package wgcore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.zx2c4.com/wireguard/device"

	"hotspot/core/internal/doh"
	"hotspot/core/internal/gateway"
	"hotspot/core/internal/pairing"
	"hotspot/core/internal/provision"
	"hotspot/core/internal/server"
	"hotspot/core/internal/wgkey"
	"hotspot/core/internal/wifiinfo"
)

// wifiDirect is the network Android gives a Wi-Fi Direct group; the phone is always 192.168.49.1 on it.
var wifiDirect = netip.MustParsePrefix("192.168.49.0/24")

const (
	defaultDNS  = "1.1.1.1:53"
	defaultPort = 51820

	// Runtime settings while the gateway runs, chosen by measuring a 200 MB
	// download on a recent flagship phone: with the defaults (8 threads, GOGC 100) the
	// CPU time per MB was about three times higher, mostly spent on waking
	// threads and on garbage collection. Two threads still move over 1 Gbit/s,
	// far more than any phone link delivers.
	tunedProcs     = 2
	tunedGCPercent = 400
	tunedMemLimit  = 256 << 20
)

// tuneRuntime applies the settings above and returns a function that restores
// the previous ones.
func tuneRuntime(procs, gcPercent int) (restore func()) {
	if n := runtime.NumCPU(); n < procs {
		procs = n
	}
	prevProcs := runtime.GOMAXPROCS(procs)
	prevGC := debug.SetGCPercent(gcPercent)
	prevLimit := debug.SetMemoryLimit(tunedMemLimit)
	return func() {
		runtime.GOMAXPROCS(prevProcs)
		debug.SetGCPercent(prevGC)
		debug.SetMemoryLimit(prevLimit)
	}
}

// Protector is implemented by the app. It is called with the file descriptor of
// every outgoing socket before it connects, and binds the socket to the network
// the traffic must leave on (the cellular network) with Network.bindSocket.
// Returning false aborts the connection.
type Protector interface {
	Protect(fd int) bool
}

// Stats holds the anonymous counters of a running core.
type Stats struct {
	TCPActive, TCPTotal int64
	UDPActive, UDPTotal int64
	DialErrors          int64
	BytesUp, BytesDown  int64
	// DNS queries of the clients: answered over HTTPS, answered in plain (no DoH server could be reached), failed.
	DNSEncrypted, DNSPlain, DNSFailed int64
	// Connections whose TLS handshake went out in pieces (see SetSplitTLS).
	TCPSplit int64
	// DNS queries answered with nothing (ads, trackers, update downloads; see SetBlockAds, SetSaveData).
	DNSBlocked int64
}

// Core is the gateway together with its list of clients.
type Core struct {
	mu       sync.Mutex
	state    *provision.State
	dns      netip.AddrPort
	allowLAN bool
	srv      *server.Server
	wgPort   int
	pairing  *pairing.Server
	// expected is the computer whose QR code the phone scanned, until it pairs or expectedUntil passes; every
	// pairing window opened meanwhile admits it (the scanner itself closes the window the screen opened).
	expected      *pairing.QRCode
	expectedUntil time.Time
	untune        func() // restores the runtime settings changed by Start
	// wifi tells paired devices the phone's Wi-Fi network; wifiConn is where it listens on that network.
	wifi     *wifiinfo.Responder
	wifiConn net.PacketConn
	rxSeen   map[wgkey.Key]rxSeen // see ActivePeers
	split    atomic.Bool          // see SetSplitTLS
	pol      policy               // see policy.go
	out      outbound             // how sharing reaches the internet while it runs (see speedtest.go)
	speed    sync.Mutex           // held while a speed test runs

	// What devices are told about the Wi-Fi network (SetWifi).
	ssid, password, nextSSID, nextPassword string
	// The releases the phone hands out and takes (see update.go), and where it serves them while sharing runs.
	upd          *updates
	updateSrv    *http.Server
	updateLn     net.Listener
	updateTunnel *tunnelListener

	procs, gcPercent int      // runtime settings used while running (see debug.go)
	profile          *os.File // CPU profile being written, if any
}

// NewCore restores a core from the JSON produced by StateJSON. An empty string
// creates a fresh one with a new server key.
func NewCore(stateJSON string) (*Core, error) {
	c := &Core{dns: netip.MustParseAddrPort(defaultDNS), wgPort: defaultPort, procs: tunedProcs, gcPercent: tunedGCPercent}
	if strings.TrimSpace(stateJSON) == "" {
		c.state = provision.NewState()
	} else {
		st, err := provision.ParseState([]byte(stateJSON))
		if err != nil {
			return nil, err
		}
		c.state = st
	}
	c.wifi = wifiinfo.NewResponder(c.state.ServerKey)
	c.upd = newUpdates()
	c.upd.changed = c.publish
	c.peersChanged()
	return c, nil
}

// publish hands the responder what devices are told now.
func (c *Core) publish() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.publishLocked()
}

// peersChanged tells the parts that know the devices about the current list. Callers hold c.mu (or own c).
func (c *Core) peersChanged() {
	keys := make([]wgkey.Key, len(c.state.Peers))
	for i, p := range c.state.Peers {
		keys[i] = p.PublicKey
	}
	c.wifi.SetPeers(keys)
}

// StateJSON returns what the app must persist. It contains no client private keys.
func (c *Core) StateJSON() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := c.state.Marshal()
	return string(data), err
}

// AddPeer registers a client and returns its WireGuard configuration (.conf
// text, ready for a QR code). The configuration holds the client's private key
// and is never available again. endpoint is the "ip:port" the client must
// connect to. The app must persist StateJSON() afterwards.
func (c *Core) AddPeer(name, endpoint string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := netip.ParseAddrPort(endpoint); err != nil {
		return "", fmt.Errorf("wgcore: endpoint %q: %w", endpoint, err)
	}
	p, priv, err := c.state.AddPeer(name)
	if err != nil {
		return "", err
	}
	if c.srv != nil {
		if err := c.srv.UpdatePeers(c.state.ServerPeers()); err != nil {
			c.state.RemovePeer(p.Name)
			return "", err
		}
	}
	c.peersChanged()
	return provision.ClientConfig(priv, p, c.state.ServerPublicKey(), endpoint), nil
}

// RemovePeer revokes a client at once and reports whether it existed. The app
// must persist StateJSON() afterwards.
func (c *Core) RemovePeer(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.state.RemovePeer(name) {
		return false
	}
	if c.srv != nil {
		// Best effort: the peer is gone from the state either way.
		c.srv.UpdatePeers(c.state.ServerPeers())
	}
	c.peersChanged()
	return true
}

// PeerNames lists the registered clients, one per line.
func (c *Core) PeerNames() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	names := make([]string, len(c.state.Peers))
	for i, p := range c.state.Peers {
		names[i] = p.Name
	}
	return strings.Join(names, "\n")
}

// SetDNS sets the "ip:port" that receives the clients' DNS queries; an empty
// string drops them. It takes effect at the next Start.
func (c *Core) SetDNS(upstream string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if upstream == "" {
		c.dns = netip.AddrPort{}
		return nil
	}
	ap, err := netip.ParseAddrPort(upstream)
	if err != nil {
		return fmt.Errorf("wgcore: dns %q: %w", upstream, err)
	}
	c.dns = ap
	return nil
}

// SetAllowLAN lets clients reach private address ranges of the phone's
// networks. Off by default. It takes effect at the next Start.
func (c *Core) SetAllowLAN(allow bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.allowLAN = allow
}

// Start brings the WireGuard server up on the given UDP port. protector may be
// nil, in which case sockets leave on the default network.
func (c *Core) Start(port int, protector Protector) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.srv != nil {
		return errors.New("wgcore: already running")
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("wgcore: bad port %d", port)
	}
	var dialer gateway.Dialer
	var dohDialer doh.Dialer
	probe := &ipv6Probe{dialer: &net.Dialer{}}
	// The releases for paired devices; without its Wi-Fi socket (port taken) the tunnel's way still works.
	tcpServices := c.startUpdates()
	if protector != nil {
		dialer = protectedDialer{protector}
		dohDialer = protectedDialer{protector}
		probe.dialer = protectedDialer{protector}
	}
	srv, err := server.Start(server.Config{
		PrivateKey: c.state.ServerKey,
		ListenPort: uint16(port),
		Peers:      c.state.ServerPeers(),
		Gateway: gateway.Options{
			MTU:    provision.TunnelMTU,
			Addrs:  []netip.Addr{provision.GatewayV4, provision.GatewayV6},
			Dialer: dialer,
			// The clients' names are looked up over HTTPS; c.dns is the plain last resort.
			Resolver:    doh.New(dohDialer, doh.DefaultServers, c.dns, nil),
			DNSUpstream: c.dns,
			// Many mobile networks are IPv4 only; then the clients are told of no IPv6 addresses at all.
			IPv6:     probe.up,
			Split:    c.split.Load,
			AllowLAN: c.allowLAN,
			// A computer whose tunnel is up asks here: its firewall lets nothing else out.
			Services:    map[uint16]func([]byte) []byte{wifiinfo.Port: c.wifi.Answer},
			TCPServices: tcpServices,
			Block:       c.blockName,
			Paused:      c.isPaused,
			Priority:    c.priorityOn,
			Limit:       c.limitsOf,
		},
		LogLevel: device.LogLevelError,
	})
	if err != nil {
		c.stopUpdates()
		return err
	}
	c.srv = srv
	c.out = outbound{dialer: dialer, dohDialer: dohDialer}
	c.wgPort = port
	c.rxSeen = map[wgkey.Key]rxSeen{} // a new server counts from zero
	c.untune = tuneRuntime(c.procs, c.gcPercent)
	// Devices without the tunnel up ask on the Wi-Fi network. Without this socket (port taken) they still learn
	// a change through the tunnel, so a failure here does not stop sharing.
	if conn, err := net.ListenPacket("udp4", fmt.Sprintf(":%d", wifiinfo.Port)); err == nil {
		c.wifiConn = conn
		go c.wifi.Serve(conn, wifiDirect.Contains)
	}
	c.publishLocked()
	return nil
}

func randomToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// SetSplitTLS switches the splitting of the first bytes of TLS handshakes on the way to the internet. It takes
// effect for the connections that start after the call, while sharing runs or later, and costs nothing while off.
// Off by default: it helps against operators whose filters read the site's name from the first segment, and
// changes how every secure connection starts.
func (c *Core) SetSplitTLS(on bool) { c.split.Store(on) }

// SplitTLS reports whether the splitting is on.
func (c *Core) SplitTLS() bool { return c.split.Load() }

// SetWifi sets what paired devices are told when they ask about the phone's Wi-Fi network: the one it opens
// (ssid, password) and, while a change is under way, the one that follows (nextSSID, nextPassword; empty when
// there is none). An empty ssid tells nothing.
func (c *Core) SetWifi(ssid, password, nextSSID, nextPassword string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ssid, c.password, c.nextSSID, c.nextPassword = ssid, password, nextSSID, nextPassword
	c.publishLocked()
}

// publishLocked hands the responder what devices are told now: the Wi-Fi network and, while sharing runs with the
// APK on offer, the app. Callers hold c.mu.
func (c *Core) publishLocked() {
	if c.ssid == "" {
		c.wifi.SetInfo(nil)
		return
	}
	info := &wifiinfo.Info{Current: wifiinfo.Network{SSID: c.ssid, Password: c.password}}
	if c.nextSSID != "" {
		info.Next = &wifiinfo.Network{SSID: c.nextSSID, Password: c.nextPassword}
	}
	if c.updateSrv != nil {
		app, pc, token := c.upd.offers()
		info.App = &wifiinfo.App{Code: app.code, Name: app.name, Token: token}
		info.PC = &wifiinfo.App{Code: pc.code, Name: pc.name, Token: token}
	}
	c.wifi.SetInfo(info)
}

// WifiLearned lists, one per line, the devices that have been told the network that follows.
func (c *Core) WifiLearned() string { return c.names(c.wifi.Learned()) }

// WifiAskers lists, one per line, the devices that have asked about the Wi-Fi network since the core was made:
// those follow a change by themselves.
func (c *Core) WifiAskers() string { return c.names(c.wifi.Asked()) }

// names lists the names of the devices with the given keys, one per line.
func (c *Core) names(keys []wgkey.Key) string {
	set := map[wgkey.Key]bool{}
	for _, k := range keys {
		set[k] = true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var names []string
	for _, p := range c.state.Peers {
		if set[p.PublicKey] {
			names = append(names, p.Name)
		}
	}
	return strings.Join(names, "\n")
}

// ConnectedPeers lists, one per line, the devices whose tunnel completed a handshake within the last seconds.
func (c *Core) ConnectedPeers(seconds int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.srv == nil {
		return ""
	}
	stats := c.srv.PeerStats()
	since := time.Now().Add(-time.Duration(seconds) * time.Second)
	var names []string
	for _, p := range c.state.Peers {
		if st, ok := stats[p.PublicKey]; ok && st.Handshake.After(since) {
			names = append(names, p.Name)
		}
	}
	return strings.Join(names, "\n")
}

// freshHandshake counts a device that has only just shaken hands as active, before it has sent much.
const freshHandshake = 30 * time.Second

// ActivePeers lists, one per line, the devices that use the tunnel now: something came from them within the last
// seconds, or they have just shaken hands. A connected device is never quiet for long: a phone's or tablet's
// tunnel sends a keepalive every 25 seconds, and a computer checks the phone once a minute. Each call also takes
// a new reading, so the answer is as fresh as the calls are frequent.
func (c *Core) ActivePeers(seconds int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.srv == nil {
		return ""
	}
	now := time.Now()
	window := time.Duration(seconds) * time.Second
	stats := c.srv.PeerStats()
	var names []string
	for _, p := range c.state.Peers {
		st, ok := stats[p.PublicKey]
		if !ok {
			continue
		}
		seen, isActive := active(st, c.rxSeen[p.PublicKey], now, window)
		c.rxSeen[p.PublicKey] = seen
		if isActive {
			names = append(names, p.Name)
		}
	}
	return strings.Join(names, "\n")
}

// PeerTraffic lists, one per line, each device's name and the bytes its flows moved to and from the internet,
// "name<TAB>bytes": what it cost of the mobile data, counted where Stats counts it, so the devices add up to
// BytesUp and BytesDown. What a device exchanges with the phone itself (the Wi-Fi speed test, releases, DNS) and
// the tunnel's own headers are not in it. The counters start again from zero when sharing starts; the app adds up
// the growth.
func (c *Core) PeerTraffic() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.srv == nil {
		return ""
	}
	return peerTraffic(c.state.Peers, c.srv.ClientBytes())
}

func peerTraffic(peers []provision.Peer, used map[netip.Addr]int64) string {
	var lines []string
	for _, p := range peers {
		lines = append(lines, fmt.Sprintf("%s\t%d", p.Name, used[p.IPv4]+used[p.IPv6]))
	}
	return strings.Join(lines, "\n")
}

// rxSeen is when the received bytes of a peer last changed, as ActivePeers noticed.
type rxSeen struct {
	bytes int64
	at    time.Time
}

// active takes a new reading of one peer and tells whether it uses the tunnel now.
func active(st server.PeerStat, seen rxSeen, now time.Time, window time.Duration) (rxSeen, bool) {
	if st.RxBytes != seen.bytes {
		seen = rxSeen{bytes: st.RxBytes, at: now}
	}
	recent := !seen.at.IsZero() && now.Sub(seen.at) < window
	fresh := !st.Handshake.IsZero() && now.Sub(st.Handshake) < freshHandshake
	return seen, recent || fresh
}

// PairingListener is implemented by the app to take part in a pairing.
type PairingListener interface {
	// Approve is called when a computer asks to join. sas is the six-digit number
	// the computer shows as well; the user must compare them. It blocks until the
	// user has decided and returns true to admit the computer.
	Approve(name, sas string) bool
	// Paired is called after a computer was added. The app must persist StateJSON().
	Paired(name string)
}

// StartPairing opens a pairing window on TCP port 51821 for three minutes. A
// computer on the same network can then join with its own key pair, and no secret
// crosses the network: the user compares its six-digit number, or the computer's
// QR code was scanned (ExpectComputer) and it joins without a question. The window
// closes after one pairing.
func (c *Core) StartPairing(l PairingListener) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.startPairing(fmt.Sprintf(":%d", pairing.Port), l)
}

func (c *Core) startPairing(addr string, l PairingListener) error {
	if l == nil {
		return errors.New("wgcore: pairing needs a listener")
	}
	if c.pairing != nil {
		select {
		case <-c.pairing.Done():
			c.pairing = nil
		default:
			return errors.New("wgcore: pairing is already open")
		}
	}
	e := enroller{c: c, l: l}
	srv, err := pairing.Listen(addr, e, e, pairing.DefaultWindow)
	if err != nil {
		return err
	}
	c.pairing = srv
	srv.Expect(c.expectedLocked())
	return nil
}

// expectWindow is how long a scanned computer code stays good.
const expectWindow = 10 * time.Minute

// ExpectComputer takes the text of a computer's pairing QR code that the user has just scanned: that computer is
// admitted without its number being asked, through the window open now or the next one, for ten minutes. The
// computer keeps asking until then. Returns an error when the text is not such a code.
func (c *Core) ExpectComputer(code string) error {
	q, err := pairing.ParseQR(code)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expected, c.expectedUntil = &q, time.Now().Add(expectWindow)
	if c.pairing != nil {
		c.pairing.Expect(&q)
	}
	return nil
}

// expectedLocked is the scanned computer still waited for, if any.
func (c *Core) expectedLocked() *pairing.QRCode {
	if c.expected != nil && time.Now().After(c.expectedUntil) {
		c.expected = nil
	}
	return c.expected
}

// StopPairing closes the pairing window.
func (c *Core) StopPairing() {
	c.mu.Lock()
	srv := c.pairing
	c.pairing = nil
	c.mu.Unlock()
	if srv != nil {
		srv.Close()
	}
}

// Pairing reports whether a pairing window is open.
func (c *Core) Pairing() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pairing == nil {
		return false
	}
	select {
	case <-c.pairing.Done():
		c.pairing = nil
		return false
	default:
		return true
	}
}

// enroller connects a pairing to the core's state and to the app.
type enroller struct {
	c *Core
	l PairingListener
}

func (e enroller) ServerPublicKey() wgkey.Key {
	e.c.mu.Lock()
	defer e.c.mu.Unlock()
	return e.c.state.ServerPublicKey()
}

func (e enroller) Approve(name, sas string) bool { return e.l.Approve(name, sas) }

func (e enroller) Enroll(name string, public wgkey.Key) (pairing.Grant, error) {
	e.c.mu.Lock()
	var previous []provision.Peer
	previous = append(previous, e.c.state.Peers...)
	p, err := e.c.state.EnrollPeer(name, public)
	if err == nil && e.c.expected != nil && e.c.expected.Public == public {
		e.c.expected = nil // the scanned computer is in
	}
	if err == nil && e.c.srv != nil {
		if err = e.c.srv.UpdatePeers(e.c.state.ServerPeers()); err != nil {
			e.c.state.Peers = previous
		}
	}
	e.c.peersChanged()
	port := e.c.wgPort
	e.c.mu.Unlock()
	if err != nil {
		return pairing.Grant{}, err
	}
	// Outside the lock: the app's callback reads the state.
	e.l.Paired(name)
	return pairing.Grant{
		IPv4: p.IPv4, IPv6: p.IPv6,
		DNS:  []netip.Addr{provision.GatewayV4, provision.GatewayV6},
		MTU:  provision.TunnelMTU,
		Port: port,
	}, nil
}

// Stop shuts the server down and tears every flow with it. A pairing window is
// independent and stays as it is.
func (c *Core) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.srv != nil {
		c.srv.Close()
		c.srv = nil
	}
	c.out = outbound{}
	if c.wifiConn != nil {
		c.wifiConn.Close()
		c.wifiConn = nil
	}
	c.stopUpdates()
	c.publishLocked()
	if c.untune != nil {
		c.untune()
		c.untune = nil
	}
}

// Running reports whether the server is up.
func (c *Core) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.srv != nil
}

// Stats returns the current counters (all zero when not running).
func (c *Core) Stats() *Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.srv == nil {
		return &Stats{}
	}
	s := c.srv.Stats()
	return &Stats{
		TCPActive: s.TCPActive, TCPTotal: s.TCPTotal,
		UDPActive: s.UDPActive, UDPTotal: s.UDPTotal,
		DialErrors: s.DialErrors,
		BytesUp:    s.BytesUp, BytesDown: s.BytesDown,
		DNSEncrypted: s.DNSEncrypted, DNSPlain: s.DNSPlain, DNSFailed: s.DNSFailed,
		TCPSplit: s.TCPSplit, DNSBlocked: s.DNSBlocked,
	}
}

// ipv6Probe tells whether the uplink has an IPv6 way out. Connecting a UDP socket sends nothing but fails at once
// when there is no IPv6 route on the network the socket is bound to; the answer is kept for a while, as networks
// rarely change their mind.
type ipv6Probe struct {
	dialer interface {
		DialContext(ctx context.Context, network, address string) (net.Conn, error)
	}

	mu      sync.Mutex
	checked time.Time
	ok      bool
}

const (
	ipv6ProbeTarget = "[2001:4860:4860::8888]:53" // any global IPv6 address: nothing is sent to it
	ipv6ProbeEvery  = time.Minute
)

func (p *ipv6Probe) up() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.checked.IsZero() && time.Since(p.checked) < ipv6ProbeEvery {
		return p.ok
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := p.dialer.DialContext(ctx, "udp6", ipv6ProbeTarget)
	if err == nil {
		conn.Close()
	}
	p.ok, p.checked = err == nil, time.Now()
	return p.ok
}

// protectedDialer hands every socket to the app before it connects.
type protectedDialer struct{ p Protector }

func (d protectedDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	nd := net.Dialer{
		Control: func(network, address string, c syscall.RawConn) error {
			var ok bool
			if err := c.Control(func(fd uintptr) { ok = d.p.Protect(int(fd)) }); err != nil {
				return err
			}
			if !ok {
				return errors.New("wgcore: socket was not protected")
			}
			return nil
		},
	}
	return nd.DialContext(ctx, network, address)
}
