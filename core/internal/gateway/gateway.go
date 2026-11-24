// Package gateway turns a WireGuard tunnel into an internet gateway without
// kernel NAT or root. Packets that come out of the tunnel are terminated in a
// user-space TCP/IP stack (gVisor) and every flow is re-originated with an
// ordinary socket obtained from Options.Dialer. On Android that dialer binds
// the sockets to the cellular network.
//
// Gateway implements tun.Device, so it can be handed to wireguard-go directly.
//
// Nothing about the flows is logged: only anonymous counters are kept.
package gateway

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/tun"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

const (
	nicID tcpip.NICID = 1

	maxInFlightSYN = 2048
	// Each flow costs an outgoing socket, buffers and goroutines. A device, or a program gone wild on one, must not
	// be able to open them without end: the app would run out of sockets for everybody.
	maxClientFlows  = 1024
	maxFlows        = 6000
	dialTimeout     = 10 * time.Second
	tcpDrainTimeout = 2 * time.Minute
	udpIdleTimeout  = time.Minute
	dnsIdleTimeout  = 10 * time.Second

	tcpBufSize = 16 << 10
	udpBufSize = 8 << 10

	// Packets move between the stack and WireGuard in batches: fewer wake-ups,
	// fewer system calls, and room for UDP segmentation offload.
	batchSize = conn.IdealBatchSize
	// A deep queue absorbs bursts instead of dropping packets, which TCP would
	// answer with a retransmission.
	queueSize = 4096
	// While a stream flows, Read waits up to the coalesce window for a batch to
	// fill, unless it already holds coalesceEnough packets. It is off by default:
	// measured on a phone with real traffic (windows from 0.25 to 2 ms), it did
	// not lower the CPU time per MB, so it would only add latency.
	defaultCoalesce = 0
	coalesceEnough  = 16

	defaultKeepAliveIdle     = 5 * time.Minute
	defaultKeepAliveInterval = 30 * time.Second
	defaultKeepAliveCount    = 4
	defaultUserTimeout       = 2 * time.Minute
)

var (
	tcpBufPool = sync.Pool{New: func() any { b := make([]byte, tcpBufSize); return &b }}
	udpBufPool = sync.Pool{New: func() any { b := make([]byte, udpBufSize); return &b }}

	limitedBroadcast = netip.AddrFrom4([4]byte{255, 255, 255, 255})
)

// Dialer opens the outgoing side of a flow.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Options configures a Gateway.
type Options struct {
	// MTU of the tunnel. Defaults to 1420.
	MTU int
	// Addrs are the gateway's own addresses inside the tunnel (IPv4 and/or IPv6).
	Addrs []netip.Addr
	// Dialer opens outgoing sockets. Defaults to a plain net.Dialer.
	Dialer Dialer
	// Resolver answers every DNS query (UDP or TCP, port 53) that a client sends,
	// to one of Addrs or to any other server: devices with a server of their own
	// built in (8.8.8.8 in TVs and streaming sticks) get answers too, encrypted
	// like everyone's, even where the uplink lets no plain DNS out. If unset, the
	// queries to Addrs go to DNSUpstream as they are, and others go out unchanged.
	Resolver Resolver
	// DNSUpstream receives the clients' DNS queries when there is no Resolver.
	// If both are unset, such queries are dropped.
	DNSUpstream netip.AddrPort
	// Split, if it says yes at the time a flow starts, makes that flow send the start of its TLS handshake in pieces
	// (see splitConn). Nil means no. It is asked once per flow, so switching it costs nothing in between.
	Split func() bool
	// IPv6 reports whether the host reaches the internet over IPv6. When it says
	// no, the Resolver's AAAA answers are withheld, so clients use IPv4 at once
	// instead of trying IPv6 addresses that cannot work. Nil means yes.
	IPv6 func() bool
	// Services answer UDP requests that a client sends to one of Addrs on their port: each datagram gets the
	// datagram the function returns back, or nothing when it returns nil.
	Services map[uint16]func(request []byte) []byte
	// TCPServices take the TCP connections a client opens to one of Addrs on their port; the function owns the
	// connection (and closes it) and may return before it is done with it. Other ports of Addrs stay closed.
	TCPServices map[uint16]func(net.Conn)
	// Block, if set, names the DNS names nobody gets an address for (ads, trackers, update downloads): their queries
	// are answered with 0.0.0.0 and :: (as ad blockers do) without asking the Resolver, and counted.
	Block func(name string) bool
	// Paused, if set, names the clients whose internet is paused: their new flows to the internet are refused, and
	// CutClient ends those under way. DNS and the gateway's own services stay, so the device keeps its tunnel.
	Paused func(client netip.Addr) bool
	// Priority, if set and on, keeps calls and games smooth: while a client sends real-time traffic, big transfers
	// are held to a share of what the link carried before (see priority).
	Priority func() bool
	// Limit, if set, returns the limiters a client's traffic to the internet goes through, TCP and UDP alike: its
	// device's and everyone's, each nil when there is none (see Limiter).
	Limit func(client netip.Addr) (device, everyone *Limiter)
	// AllowLAN lets clients reach private address ranges of the host's network.
	// Off by default: a client must not be able to probe the host's LAN.
	AllowLAN bool
	// AllowLoopback lets clients reach the host's loopback. For tests only.
	AllowLoopback bool
	// KeepAlive* probe the client side of every TCP flow, so that the flows of
	// a client that vanished (switched off, out of range) are closed instead of
	// living forever. Defaults: 5 minutes idle, then 4 probes 30 seconds apart.
	KeepAliveIdle     time.Duration
	KeepAliveInterval time.Duration
	KeepAliveCount    int
	// UserTimeout ends a TCP flow whose data stays unacknowledged for this long:
	// the keepalive does not run while data is in flight, so a client that vanishes
	// mid-download is caught by this instead. Default 2 minutes.
	UserTimeout time.Duration
}

// Resolver answers DNS messages (see the doh package).
type Resolver interface {
	// Resolve returns the answer to one DNS message, and whether it travelled encrypted.
	Resolve(ctx context.Context, query []byte) (answer []byte, encrypted bool, err error)
}

// Stats is a snapshot of the anonymous counters.
type Stats struct {
	TCPActive, TCPTotal int64
	UDPActive, UDPTotal int64
	DialErrors          int64
	BytesUp, BytesDown  int64 // client to internet, internet to client
	// DNS queries answered by the Resolver: encrypted, in plain (its last resort), or not at all.
	DNSEncrypted, DNSPlain, DNSFailed int64
	// TCPSplit counts the flows whose TLS handshake went out in pieces (see Options.Split).
	TCPSplit int64
	// DNSBlocked counts the queries for blocked names (see Options.Block).
	DNSBlocked int64
}

type counters struct {
	tcpSplit                          atomic.Int64
	dnsBlocked                        atomic.Int64
	tcpActive, tcpTotal               atomic.Int64
	udpActive, udpTotal               atomic.Int64
	dialErrors                        atomic.Int64
	bytesUp, bytesDown                atomic.Int64
	dnsEncrypted, dnsPlain, dnsFailed atomic.Int64
}

// Gateway is a tun.Device that forwards what it receives to the internet.
type Gateway struct {
	opts   Options
	stack  *stack.Stack
	ep     *channel.Endpoint
	notify *channel.NotificationHandle
	addrs  map[netip.Addr]struct{}

	events    chan tun.Event
	wake      chan struct{} // the stack has packets for WireGuard
	streaming bool          // the last batch held several packets; only Read touches it
	done      chan struct{}
	closeOnce sync.Once

	stats counters
	flows flowSet   // the flows under way per client, for CutClient
	usage usage     // the bytes per client, for ClientBytes
	prio  *priority // see Options.Priority
}

// New creates a Gateway.
func New(opts Options) (*Gateway, error) {
	if opts.MTU == 0 {
		opts.MTU = 1420
	}
	if opts.Dialer == nil {
		opts.Dialer = &net.Dialer{}
	}
	if opts.KeepAliveIdle == 0 {
		opts.KeepAliveIdle = defaultKeepAliveIdle
	}
	if opts.KeepAliveInterval == 0 {
		opts.KeepAliveInterval = defaultKeepAliveInterval
	}
	if opts.KeepAliveCount == 0 {
		opts.KeepAliveCount = defaultKeepAliveCount
	}
	if opts.UserTimeout == 0 {
		opts.UserTimeout = defaultUserTimeout
	}

	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol4, icmp.NewProtocol6},
	})
	g := &Gateway{
		opts:   opts,
		stack:  s,
		ep:     channel.New(queueSize, uint32(opts.MTU), ""),
		addrs:  make(map[netip.Addr]struct{}),
		events: make(chan tun.Event, 10),
		wake:   make(chan struct{}, 1),
		done:   make(chan struct{}),
		prio:   &priority{on: opts.Priority},
	}
	// Whatever arrives from a client was authenticated by WireGuard, so checking
	// its TCP/UDP checksums again would only burn CPU.
	g.ep.LinkEPCapabilities = stack.CapabilityRXChecksumOffload

	sack := tcpip.TCPSACKEnabled(true) // disabled by default in gVisor
	if terr := s.SetTransportProtocolOption(tcp.ProtocolNumber, &sack); terr != nil {
		return nil, errors.New("gateway: enable TCP SACK: " + terr.String())
	}

	g.notify = g.ep.AddNotify(g)
	if terr := s.CreateNIC(nicID, g.ep); terr != nil {
		return nil, errors.New("gateway: create NIC: " + terr.String())
	}
	// Promiscuous + spoofing: accept packets for any destination and answer
	// from the destination's address, which is what makes this a gateway.
	if terr := s.SetPromiscuousMode(nicID, true); terr != nil {
		return nil, errors.New("gateway: promiscuous mode: " + terr.String())
	}
	if terr := s.SetSpoofing(nicID, true); terr != nil {
		return nil, errors.New("gateway: spoofing: " + terr.String())
	}

	for _, ip := range opts.Addrs {
		proto := ipv4.ProtocolNumber
		if ip.Is6() {
			proto = ipv6.ProtocolNumber
		}
		pa := tcpip.ProtocolAddress{
			Protocol:          proto,
			AddressWithPrefix: tcpip.AddrFromSlice(ip.AsSlice()).WithPrefix(),
		}
		if terr := s.AddProtocolAddress(nicID, pa, stack.AddressProperties{}); terr != nil {
			return nil, errors.New("gateway: add address " + ip.String() + ": " + terr.String())
		}
		g.addrs[ip.Unmap()] = struct{}{}
	}
	s.AddRoute(tcpip.Route{Destination: header.IPv4EmptySubnet, NIC: nicID})
	s.AddRoute(tcpip.Route{Destination: header.IPv6EmptySubnet, NIC: nicID})

	tcpFwd := tcp.NewForwarder(s, 0, maxInFlightSYN, g.handleTCP)
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpFwd.HandlePacket)
	udpFwd := udp.NewForwarder(s, g.handleUDP)
	s.SetTransportProtocolHandler(udp.ProtocolNumber, udpFwd.HandlePacket)

	g.events <- tun.EventUp
	return g, nil
}

// Stats returns the current counters.
func (g *Gateway) Stats() Stats {
	return Stats{
		TCPActive:  g.stats.tcpActive.Load(),
		TCPTotal:   g.stats.tcpTotal.Load(),
		UDPActive:  g.stats.udpActive.Load(),
		UDPTotal:   g.stats.udpTotal.Load(),
		DialErrors: g.stats.dialErrors.Load(),
		BytesUp:    g.stats.bytesUp.Load(),
		BytesDown:  g.stats.bytesDown.Load(),

		DNSEncrypted: g.stats.dnsEncrypted.Load(),
		DNSPlain:     g.stats.dnsPlain.Load(),
		DNSFailed:    g.stats.dnsFailed.Load(),
		TCPSplit:     g.stats.tcpSplit.Load(),
		DNSBlocked:   g.stats.dnsBlocked.Load(),
	}
}

// localDNS reports whether a flow is a DNS query that the Resolver answers itself: any query, whichever server
// the client meant it for.
func (g *Gateway) localDNS(dst netip.AddrPort) bool {
	return dst.Port() == 53 && g.opts.Resolver != nil
}

// service returns the gateway's own UDP service a flow goes to, if any.
func (g *Gateway) service(dst netip.AddrPort) func([]byte) []byte {
	if _, ours := g.addrs[dst.Addr()]; !ours {
		return nil
	}
	return g.opts.Services[dst.Port()]
}

// tcpService returns the gateway's own TCP service a flow goes to, if any.
func (g *Gateway) tcpService(dst netip.AddrPort) func(net.Conn) {
	if _, ours := g.addrs[dst.Addr()]; !ours {
		return nil
	}
	return g.opts.TCPServices[dst.Port()]
}

// resolve returns where a flow addressed to dst must really go, and whether it
// may go there at all.
func (g *Gateway) resolve(dst netip.AddrPort) (netip.AddrPort, bool) {
	if _, ours := g.addrs[dst.Addr()]; ours {
		if dst.Port() == 53 && g.opts.DNSUpstream.IsValid() {
			return g.opts.DNSUpstream, true
		}
		return netip.AddrPort{}, false
	}
	return dst, g.allowed(dst.Addr())
}

func (g *Gateway) allowed(a netip.Addr) bool {
	switch {
	case a.IsUnspecified(), a.IsMulticast(), a.IsLinkLocalUnicast(), a == limitedBroadcast:
		return false
	case a.IsLoopback():
		return g.opts.AllowLoopback
	case a.IsPrivate():
		return g.opts.AllowLAN
	}
	return true
}

func addrPort(a tcpip.Address, port uint16) (netip.AddrPort, bool) {
	ip, ok := netip.AddrFromSlice(a.AsSlice())
	if !ok {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(ip.Unmap(), port), true
}

func (g *Gateway) handleTCP(r *tcp.ForwarderRequest) {
	id := r.ID()
	dst, ok := addrPort(id.LocalAddress, id.LocalPort)
	if !ok {
		r.Complete(true)
		return
	}
	if g.localDNS(dst) {
		var wq waiter.Queue
		ep, terr := r.CreateEndpoint(&wq)
		if terr != nil {
			r.Complete(true)
			return
		}
		r.Complete(false)
		g.stats.tcpTotal.Add(1)
		g.stats.tcpActive.Add(1)
		defer g.stats.tcpActive.Add(-1)
		g.serveDNSTCP(gonet.NewTCPConn(&wq, ep))
		return
	}
	if serve := g.tcpService(dst); serve != nil {
		var wq waiter.Queue
		ep, terr := r.CreateEndpoint(&wq)
		if terr != nil {
			r.Complete(true)
			return
		}
		r.Complete(false)
		serve(gonet.NewTCPConn(&wq, ep))
		return
	}
	client, _ := addrPort(id.RemoteAddress, id.RemotePort)
	target, allowed := g.resolve(dst)
	if !allowed || g.paused(client.Addr()) || g.flows.full(client.Addr()) {
		r.Complete(true)
		return
	}

	// The forwarder runs us in our own goroutine, so we can dial first and
	// only then accept: a failed dial turns into a RST the client sees at once.
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	out, err := g.opts.Dialer.DialContext(ctx, "tcp", target.String())
	cancel()
	if err != nil {
		g.stats.dialErrors.Add(1)
		r.Complete(true)
		return
	}

	var wq waiter.Queue
	ep, terr := r.CreateEndpoint(&wq)
	if terr != nil {
		out.Close()
		r.Complete(true)
		return
	}
	r.Complete(false)
	// Probe the client side: if the client vanishes without a FIN, the flow
	// ends once the probes go unanswered.
	ep.SocketOptions().SetKeepAlive(true)
	idle := tcpip.KeepaliveIdleOption(g.opts.KeepAliveIdle)
	ep.SetSockOpt(&idle)
	interval := tcpip.KeepaliveIntervalOption(g.opts.KeepAliveInterval)
	ep.SetSockOpt(&interval)
	ep.SetSockOptInt(tcpip.KeepaliveCountOption, g.opts.KeepAliveCount)
	userTimeout := tcpip.TCPUserTimeoutOption(g.opts.UserTimeout)
	ep.SetSockOpt(&userTimeout)
	in := &stackConn{TCPConn: gonet.NewTCPConn(&wq, ep), ep: ep}
	if g.opts.Split != nil && g.opts.Split() {
		out = &splitConn{Conn: out, count: &g.stats.tcpSplit}
	}

	g.stats.tcpTotal.Add(1)
	g.stats.tcpActive.Add(1)
	defer g.stats.tcpActive.Add(-1)
	g.relayTCP(in, out, client.Addr())
}

// stackConn is the client's end of a TCP flow, with the one thing gonet leaves out: closing it as broken.
type stackConn struct {
	*gonet.TCPConn
	ep tcpip.Endpoint
}

// SetLinger is net.TCPConn's: after SetLinger(0), Close resets the connection.
func (c *stackConn) SetLinger(sec int) error {
	c.ep.SocketOptions().SetLinger(tcpip.LingerOption{Enabled: sec >= 0, Timeout: time.Duration(max(sec, 0)) * time.Second})
	return nil
}

// abort closes c so that its other end sees the connection break (a reset) and not end. A connection that only
// knows how to close is closed.
func abort(c net.Conn) {
	for inner := c; inner != nil; {
		if l, ok := inner.(interface{ SetLinger(int) error }); ok {
			l.SetLinger(0)
			break
		}
		w, ok := inner.(interface{ Unwrap() net.Conn })
		if !ok {
			break
		}
		inner = w.Unwrap()
	}
	c.Close()
}

// relayTCP copies both directions until both ended, an error occurs, or one
// direction has been finished for longer than tcpDrainTimeout.
//
// A flow that breaks (one side reset it or went silent, or the device was paused) is passed on as broken, a reset,
// to both sides. Closing it the plain way told the client that the server had finished: a download cut half-way
// then looked complete to programs that take the end of the connection for the end of the file.
func (g *Gateway) relayTCP(in, out net.Conn, client netip.Addr) {
	var once sync.Once
	end := func(broken bool) {
		once.Do(func() {
			if broken {
				abort(in)
				abort(out)
				return
			}
			in.Close()
			out.Close()
		})
	}
	closeBoth, breakBoth := func() { end(false) }, func() { end(true) }
	defer closeBoth()
	defer g.flows.add(client, breakBoth)()
	if g.paused(client) {
		return // paused while the connection was being made: the cut that came with the pause did not see it yet
	}

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		timers []*time.Timer
	)
	half := func(dst, src net.Conn, dir int, client netip.Addr) {
		defer wg.Done()
		if err := g.pipe(dst, src, dir, client); err != nil {
			breakBoth()
			return
		}
		// Clean EOF: pass the FIN on and give the other direction bounded time.
		cw, ok := dst.(interface{ CloseWrite() error })
		if !ok {
			closeBoth()
			return
		}
		cw.CloseWrite()
		mu.Lock()
		timers = append(timers, time.AfterFunc(tcpDrainTimeout, closeBoth))
		mu.Unlock()
	}
	wg.Add(2)
	go half(out, in, up, client)
	go half(in, out, down, client)
	wg.Wait()

	mu.Lock()
	for _, t := range timers {
		t.Stop()
	}
	mu.Unlock()
}

// pipe copies src to dst in one direction (down or up) for client and returns nil on a clean EOF. A big transfer
// waits a little between reads while the priority holds it back (see priority), and any flow does when its device
// or everyone has a speed limit (see Options.Limit).
func (g *Gateway) pipe(dst, src net.Conn, dir int, client netip.Addr) error {
	ctr := &g.stats.bytesDown
	if dir == up {
		ctr = &g.stats.bytesUp
	}
	used := g.usage.of(client)
	bp := tcpBufPool.Get().(*[]byte)
	defer tcpBufPool.Put(bp)
	buf := *bp
	var moved int64
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
			ctr.Add(int64(n))
			used.Add(int64(n))
			moved += int64(n)
			now := time.Now()
			g.prio.carried(dir, n, now)
			if wait := max(g.prio.hold(dir, moved, n, now), g.limitWait(client, dir, n, now)); wait > 0 {
				time.Sleep(wait)
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return nil
			}
			return rerr
		}
	}
}

func (g *Gateway) handleUDP(r *udp.ForwarderRequest) {
	// Called synchronously by the stack: must not block.
	id := r.ID()
	dst, ok := addrPort(id.LocalAddress, id.LocalPort)
	if !ok {
		return
	}
	local := g.localDNS(dst)
	service := g.service(dst)
	client, _ := addrPort(id.RemoteAddress, id.RemotePort)
	target, allowed := g.resolve(dst)
	if (!allowed || g.paused(client.Addr()) || g.flows.full(client.Addr())) && !local && service == nil {
		return
	}
	var wq waiter.Queue
	ep, terr := r.CreateEndpoint(&wq)
	if terr != nil {
		return
	}
	in := gonet.NewUDPConn(&wq, ep)

	g.stats.udpTotal.Add(1)
	g.stats.udpActive.Add(1)
	switch {
	case local:
		go g.serveDNSUDP(in)
	case service != nil:
		go g.serveUDP(in, service)
	default:
		go g.relayUDP(in, target, client.Addr())
	}
}

// serveUDP answers the datagrams of one client flow with a service of the gateway until the flow falls idle.
func (g *Gateway) serveUDP(in net.Conn, service func([]byte) []byte) {
	defer g.stats.udpActive.Add(-1)
	defer in.Close()
	bp := udpBufPool.Get().(*[]byte)
	defer udpBufPool.Put(bp)
	buf := *bp
	for {
		in.SetReadDeadline(time.Now().Add(dnsIdleTimeout))
		n, err := in.Read(buf)
		if err != nil {
			return
		}
		if ans := service(buf[:n]); ans != nil {
			if _, err := in.Write(ans); err != nil {
				return
			}
		}
	}
}

// answer asks the Resolver and counts how it went; a failure becomes a SERVFAIL, so the client gives up at once
// instead of waiting for its own timeout.
func (g *Gateway) answer(query []byte) []byte {
	if g.blocked(query) {
		g.stats.dnsBlocked.Add(1)
		return blockedAnswer(query)
	}
	if g.opts.IPv6 != nil && asksAAAA(query) && !g.opts.IPv6() {
		// "This name has no IPv6 address": the client goes for the IPv4 one without trying what cannot work.
		return emptyAnswer(query, dnsmessage.RCodeSuccess)
	}
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	ans, encrypted, err := g.opts.Resolver.Resolve(ctx, query)
	switch {
	case err != nil:
		g.stats.dnsFailed.Add(1)
		return servfail(query)
	case encrypted:
		g.stats.dnsEncrypted.Add(1)
	default:
		g.stats.dnsPlain.Add(1)
	}
	return ans
}

// serveDNSUDP answers the queries of one client flow until it falls idle.
func (g *Gateway) serveDNSUDP(in net.Conn) {
	defer g.stats.udpActive.Add(-1)
	defer in.Close()
	bp := udpBufPool.Get().(*[]byte)
	defer udpBufPool.Put(bp)
	buf := *bp
	for {
		in.SetReadDeadline(time.Now().Add(dnsIdleTimeout))
		n, err := in.Read(buf)
		if err != nil {
			return
		}
		g.stats.bytesUp.Add(int64(n))
		query := append([]byte(nil), buf[:n]...)
		ans := fitUDP(query, g.answer(query))
		if ans == nil {
			continue
		}
		if _, err := in.Write(ans); err != nil {
			return
		}
		g.stats.bytesDown.Add(int64(len(ans)))
	}
}

// serveDNSTCP answers length-prefixed queries on one connection until the client closes it or falls idle.
func (g *Gateway) serveDNSTCP(in net.Conn) {
	defer in.Close()
	var size [2]byte
	for {
		in.SetReadDeadline(time.Now().Add(dnsIdleTimeout))
		if _, err := io.ReadFull(in, size[:]); err != nil {
			return
		}
		query := make([]byte, int(size[0])<<8|int(size[1]))
		if _, err := io.ReadFull(in, query); err != nil {
			return
		}
		g.stats.bytesUp.Add(int64(len(query) + 2))
		ans := g.answer(query)
		if ans == nil || len(ans) > 0xffff {
			return
		}
		out := append([]byte{byte(len(ans) >> 8), byte(len(ans))}, ans...)
		if _, err := in.Write(out); err != nil {
			return
		}
		g.stats.bytesDown.Add(int64(len(out)))
	}
}

// fitUDP keeps an answer within what the client said it takes over UDP: 512 bytes, or more with EDNS0. A larger
// one is cut down to a truncated answer, which makes the client ask again over TCP.
func fitUDP(query, answer []byte) []byte {
	if answer == nil {
		return nil
	}
	limit := 512
	var p dnsmessage.Parser
	if _, err := p.Start(query); err == nil && p.SkipAllQuestions() == nil && p.SkipAllAnswers() == nil && p.SkipAllAuthorities() == nil {
		for {
			h, err := p.AdditionalHeader()
			if err != nil {
				break
			}
			if h.Type == dnsmessage.TypeOPT {
				limit = max(limit, int(h.Class))
				break
			}
			if p.SkipAdditional() != nil {
				break
			}
		}
	}
	if len(answer) <= limit {
		return answer
	}
	var ap dnsmessage.Parser
	hdr, err := ap.Start(answer)
	if err != nil {
		return nil
	}
	questions, _ := ap.AllQuestions()
	hdr.Truncated = true
	b := dnsmessage.NewBuilder(nil, hdr)
	b.StartQuestions()
	for _, q := range questions {
		b.Question(q)
	}
	out, err := b.Finish()
	if err != nil {
		return nil
	}
	return out
}

// servfail is the answer "the server failed" to query, or nil if query cannot even be read.
func servfail(query []byte) []byte { return emptyAnswer(query, dnsmessage.RCodeServerFailure) }

// asksAAAA reports whether a DNS query asks for IPv6 addresses.
func asksAAAA(query []byte) bool {
	var p dnsmessage.Parser
	if _, err := p.Start(query); err != nil {
		return false
	}
	q, err := p.Question()
	return err == nil && q.Type == dnsmessage.TypeAAAA
}

// emptyAnswer answers a query with its questions and no records: SERVFAIL, or with RCodeSuccess "no such record".
func emptyAnswer(query []byte, rcode dnsmessage.RCode) []byte {
	var p dnsmessage.Parser
	hdr, err := p.Start(query)
	if err != nil {
		return nil
	}
	questions, _ := p.AllQuestions()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{
		ID: hdr.ID, Response: true, OpCode: hdr.OpCode, RecursionDesired: hdr.RecursionDesired,
		RecursionAvailable: true, RCode: rcode,
	})
	b.StartQuestions()
	for _, q := range questions {
		b.Question(q)
	}
	out, err := b.Finish()
	if err != nil {
		return nil
	}
	return out
}

// relayUDP shuttles datagrams of one flow (one client 4-tuple) until it has
// been idle for too long.
func (g *Gateway) relayUDP(in net.Conn, target netip.AddrPort, client netip.Addr) {
	defer g.stats.udpActive.Add(-1)
	defer in.Close()

	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	out, err := g.opts.Dialer.DialContext(ctx, "udp", target.String())
	cancel()
	if err != nil {
		g.stats.dialErrors.Add(1)
		return
	}
	defer out.Close()
	defer g.flows.add(client, func() { in.Close(); out.Close() })()
	if g.paused(client) {
		return // paused while the socket was being opened
	}
	used := g.usage.of(client)
	// A call or a game: a steady stream of small datagrams from the client turns the priority on.
	var window rtWindow
	sawPacket := func(n int) {
		window.add(n, time.Now(), func(packets, bytes int) {
			g.prio.sawRealtime(packets, bytes, target.Port(), time.Now())
		})
	}

	idle := udpIdleTimeout
	if target.Port() == 53 {
		idle = dnsIdleTimeout
	}
	var last atomic.Int64
	last.Store(time.Now().UnixNano())

	pump := func(dst, src net.Conn, ctr *atomic.Int64, fromClient bool, done chan<- struct{}) {
		dir := down
		if fromClient {
			dir = up
		}
		defer func() { done <- struct{}{} }()
		bp := udpBufPool.Get().(*[]byte)
		defer udpBufPool.Put(bp)
		buf := *bp
		for {
			src.SetReadDeadline(time.Now().Add(idle))
			n, err := src.Read(buf)
			if n > 0 {
				last.Store(time.Now().UnixNano())
				if _, werr := dst.Write(buf[:n]); werr != nil {
					return
				}
				ctr.Add(int64(n))
				used.Add(int64(n))
				if fromClient {
					sawPacket(n)
				}
				// A limited device's QUIC (video, mostly) waits too: datagrams that pile up meanwhile are dropped,
				// and the sender slows down.
				if wait := g.limitWait(client, dir, n, time.Now()); wait > 0 {
					time.Sleep(wait)
				}
			}
			if err != nil {
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() && time.Since(time.Unix(0, last.Load())) < idle {
					continue // the other direction is still active
				}
				return
			}
		}
	}
	done := make(chan struct{}, 2)
	go pump(out, in, &g.stats.bytesUp, true, done)
	go pump(in, out, &g.stats.bytesDown, false, done)
	<-done
	in.Close()
	out.Close()
	<-done
}

// tun.Device implementation.

func (g *Gateway) Name() (string, error) { return "gateway", nil }

func (g *Gateway) File() *os.File { return nil }

func (g *Gateway) Events() <-chan tun.Event { return g.events }

func (g *Gateway) MTU() (int, error) { return g.opts.MTU, nil }

func (g *Gateway) BatchSize() int { return batchSize }

// Read hands wireguard-go the packets that the stack wants to send to clients:
// it waits for the first one, then takes everything that is already queued, up
// to a full batch. Packets are copied straight from the stack's buffers into
// WireGuard's, with nothing allocated on the way.
func (g *Gateway) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	n := 0
	waited := false
	for n < len(bufs) {
		pkt := g.ep.Read()
		if pkt == nil {
			if n == 0 {
				select {
				case <-g.wake:
				case <-g.done:
					return 0, os.ErrClosed
				}
				continue
			}
			// The queue is empty. While a stream flows (the last batch held several
			// packets), pause briefly so that more packets join this batch: sending a
			// batch costs one system call and, with UDP segmentation offload, a whole
			// batch of equal-sized packets leaves in one. At phone speeds packets
			// arrive too far apart to batch themselves, and a system call per packet
			// was the largest single cost. Sparse traffic is not delayed at all.
			if w := time.Duration(coalesceNanos.Load()); !waited && g.streaming && w > 0 && n < coalesceEnough {
				waited = true
				time.Sleep(w)
				continue
			}
			break
		}
		sizes[n] = copyPacket(bufs[n][offset:], pkt)
		pkt.DecRef()
		n++
	}
	g.streaming = n >= 2
	return n, nil
}

// coalesceNanos is how long Read waits for more packets while a stream flows.
var coalesceNanos atomic.Int64

func init() { coalesceNanos.Store(int64(defaultCoalesce)) }

// SetCoalesceWindow changes how long a flowing stream waits to fill a batch
// (zero turns the wait off). It exists for measuring.
func SetCoalesceWindow(d time.Duration) { coalesceNanos.Store(int64(d)) }

// copyPacket copies a packet, headers included, into dst and returns its length.
func copyPacket(dst []byte, pkt *stack.PacketBuffer) int {
	views, skip := pkt.AsViewList()
	n := 0
	for v := views.Front(); v != nil; v = v.Next() {
		s := v.AsSlice()
		if skip >= len(s) {
			skip -= len(s)
			continue
		}
		n += copy(dst[n:], s[skip:])
		skip = 0
	}
	return n
}

// Write injects packets that arrived from a client into the stack.
func (g *Gateway) Write(bufs [][]byte, offset int) (int, error) {
	for _, buf := range bufs {
		packet := buf[offset:]
		if len(packet) == 0 {
			continue
		}
		pkb := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(packet)})
		switch packet[0] >> 4 {
		case 4:
			g.ep.InjectInbound(header.IPv4ProtocolNumber, pkb)
		case 6:
			g.ep.InjectInbound(header.IPv6ProtocolNumber, pkb)
		default:
			return 0, syscall.EAFNOSUPPORT
		}
	}
	return len(bufs), nil
}

// WriteNotify is called by the link endpoint when the stack has a packet to
// send. It only wakes Read up; one wake-up covers every packet queued meanwhile.
func (g *Gateway) WriteNotify() {
	select {
	case g.wake <- struct{}{}:
	default:
	}
}

// Close shuts the stack down. Existing flows are torn down with it.
func (g *Gateway) Close() error {
	g.closeOnce.Do(func() {
		close(g.done)
		g.stack.RemoveNIC(nicID)
		g.stack.Close()
		g.ep.RemoveNotify(g.notify)
		g.ep.Close()
		close(g.events)
	})
	return nil
}
