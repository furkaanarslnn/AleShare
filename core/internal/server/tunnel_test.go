package server_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"runtime"
	"runtime/metrics"
	"runtime/pprof"
	"sync/atomic"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/conn/bindtest"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"

	"hotspot/core/internal/gateway"
	"hotspot/core/internal/server"
	"hotspot/core/internal/wgkey"
)

// tunnel is a client and a server joined by a real WireGuard tunnel over
// loopback, both in this process.
type tunnel struct {
	srv    *server.Server
	tnet   *netstack.Net
	client *device.Device
}

func freeUDPPortTB(tb testing.TB) uint16 {
	tb.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	defer c.Close()
	return uint16(c.LocalAddr().(*net.UDPAddr).Port)
}

func newTunnel(tb testing.TB, d gateway.Dialer, opts gateway.Options) *tunnel {
	tb.Helper()
	var (
		gw4       = netip.MustParseAddr("10.8.0.1")
		gw6       = netip.MustParseAddr("fd42:42:42::1")
		client4   = netip.MustParseAddr("10.8.0.2")
		client6   = netip.MustParseAddr("fd42:42:42::2")
		serverKey = wgkey.Generate()
		clientKey = wgkey.Generate()
		port      = freeUDPPortTB(tb)
	)
	opts.Addrs = []netip.Addr{gw4, gw6}
	opts.Dialer = d
	srv, err := server.Start(server.Config{
		PrivateKey: serverKey,
		ListenPort: port,
		Peers:      []server.Peer{{PublicKey: clientKey.Public(), IPv4: client4, IPv6: client6}},
		Gateway:    opts,
		LogLevel:   device.LogLevelError,
	})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(srv.Close)

	ctun, tnet, err := netstack.CreateNetTUN([]netip.Addr{client4, client6}, []netip.Addr{gw4}, 1420)
	if err != nil {
		tb.Fatal(err)
	}
	cdev := device.NewDevice(ctun, conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, "client: "))
	tb.Cleanup(cdev.Close)
	err = cdev.IpcSet(fmt.Sprintf("private_key=%s\npublic_key=%s\nendpoint=127.0.0.1:%d\nallowed_ip=0.0.0.0/0\nallowed_ip=::/0\n",
		clientKey.Hex(), serverKey.Public().Hex(), port))
	if err != nil {
		tb.Fatal(err)
	}
	if err := cdev.Up(); err != nil {
		tb.Fatal(err)
	}
	return &tunnel{srv: srv, tnet: tnet, client: cdev}
}

func waitFor(t *testing.T, what string, limit time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// fastProbes shortens the keepalive for tests. The interval must stay above the
// 500 ms that TCP stacks (gVisor, Linux) wait between answers to out-of-window
// segments, and a keepalive probe is such a segment.
var fastProbes = gateway.Options{
	KeepAliveIdle:     600 * time.Millisecond,
	KeepAliveInterval: 600 * time.Millisecond,
	KeepAliveCount:    2,
}

// A client that vanishes without closing anything (switched off, out of range)
// must not leave its flows open on the gateway for ever.
func TestVanishedClientFlowsAreReaped(t *testing.T) {
	echo := startTCPEcho(t)
	tn := newTunnel(t, &fakeDialer{tcpAddr: echo, seen: make(chan string, 16)}, fastProbes)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i := 0; i < 3; i++ {
		c, err := tn.tnet.DialContextTCPAddrPort(ctx, netip.MustParseAddrPort("203.0.113.5:80"))
		if err != nil {
			t.Fatal(err)
		}
		c.Write([]byte("ping"))
		buf := make([]byte, 4)
		if _, err := io.ReadFull(c, buf); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "three active flows", 5*time.Second, func() bool { return tn.srv.Stats().TCPActive == 3 })
	// Let the client's delayed ACKs reach the gateway. With data still in flight
	// the keepalive does not run (see TestVanishedClientMidDownloadIsReaped).
	time.Sleep(700 * time.Millisecond)

	tn.client.Close() // gone: no FIN, no RST

	waitFor(t, "the flows of the vanished client to be closed", 20*time.Second, func() bool {
		return tn.srv.Stats().TCPActive == 0
	})
}

// A client that vanishes in the middle of a download leaves unacknowledged data
// behind, so the keepalive is silent; the user timeout must end the flow.
func TestVanishedClientMidDownloadIsReaped(t *testing.T) {
	tn := newTunnel(t, sourceDialer{}, gateway.Options{UserTimeout: 1500 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := tn.tnet.DialContextTCPAddrPort(ctx, netip.MustParseAddrPort("203.0.113.5:80"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(c, make([]byte, 256<<10)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the download to be active", 5*time.Second, func() bool { return tn.srv.Stats().TCPActive == 1 })

	tn.client.Close() // gone while the gateway still has data to send

	waitFor(t, "the interrupted download to be closed", 30*time.Second, func() bool {
		return tn.srv.Stats().TCPActive == 0
	})
}

// The probes must not kill a flow whose client is alive but silent.
func TestIdleButAliveFlowSurvives(t *testing.T) {
	cases := []struct {
		name string
		opts gateway.Options
	}{
		{"default keepalive", gateway.Options{}},
		{"fast probes", fastProbes},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			echo := startTCPEcho(t)
			tn := newTunnel(t, &fakeDialer{tcpAddr: echo, seen: make(chan string, 16)}, c.opts)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			conn, err := tn.tnet.DialContextTCPAddrPort(ctx, netip.MustParseAddrPort("203.0.113.5:80"))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()

			time.Sleep(6 * time.Second) // several times idle + probes of the fast case
			conn.SetDeadline(time.Now().Add(10 * time.Second))
			if _, err := conn.Write([]byte("still here")); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, len("still here"))
			if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "still here" {
				t.Fatalf("flow died while idle: %v %q", err, buf)
			}
			if got := tn.srv.Stats().TCPActive; got != 1 {
				t.Fatalf("TCPActive = %d", got)
			}
		})
	}
}

// ---- benchmark: the downlink, where a phone spends its CPU ----

// source is an endless supply of bytes standing in for a fast web server.
type source struct{ closed atomic.Bool }

func (s *source) Read(p []byte) (int, error) {
	if s.closed.Load() {
		return 0, io.EOF
	}
	return len(p), nil
}
func (s *source) Write(p []byte) (int, error)      { return len(p), nil }
func (s *source) Close() error                     { s.closed.Store(true); return nil }
func (s *source) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (s *source) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (s *source) SetDeadline(time.Time) error      { return nil }
func (s *source) SetReadDeadline(time.Time) error  { return nil }
func (s *source) SetWriteDeadline(time.Time) error { return nil }

type sourceDialer struct{}

func (sourceDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" {
		return nil, errors.New("only tcp")
	}
	return &source{}, nil
}

// cpuSeconds is the CPU time this process spent running Go code, GC included.
func cpuSeconds() float64 {
	s := []metrics.Sample{{Name: "/cpu/classes/user:cpu-seconds"}, {Name: "/cpu/classes/gc/total:cpu-seconds"}}
	metrics.Read(s)
	var total float64
	for _, x := range s {
		if x.Value.Kind() == metrics.KindFloat64 {
			total += x.Value.Float64()
		}
	}
	return total
}

// newMemTunnel is newTunnel with the encrypted packets carried in memory instead of over loopback UDP, which on
// the test computer (Windows) costs more than everything else together and tells nothing about a phone. The
// server is started under the profiler label side=server, which every goroutine it starts inherits: a CPU
// profile of a benchmark can then be narrowed to the gateway's share (go tool pprof -tagfocus side=server).
func newMemTunnel(tb testing.TB, d gateway.Dialer, opts gateway.Options) *tunnel {
	tb.Helper()
	var (
		gw4       = netip.MustParseAddr("10.8.0.1")
		gw6       = netip.MustParseAddr("fd42:42:42::1")
		client4   = netip.MustParseAddr("10.8.0.2")
		client6   = netip.MustParseAddr("fd42:42:42::2")
		serverKey = wgkey.Generate()
		clientKey = wgkey.Generate()
		binds     = bindtest.NewChannelBinds()
	)
	opts.Addrs = []netip.Addr{gw4, gw6}
	opts.Dialer = d
	var srv *server.Server
	var err error
	pprof.Do(context.Background(), pprof.Labels("side", "server"), func(context.Context) {
		srv, err = server.Start(server.Config{
			PrivateKey: serverKey,
			Peers:      []server.Peer{{PublicKey: clientKey.Public(), IPv4: client4, IPv6: client6}},
			Gateway:    opts,
			LogLevel:   device.LogLevelError,
			Bind:       binds[0],
		})
	})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(srv.Close)

	ctun, tnet, err := netstack.CreateNetTUN([]netip.Addr{client4, client6}, []netip.Addr{gw4}, 1420)
	if err != nil {
		tb.Fatal(err)
	}
	cdev := device.NewDevice(ctun, binds[1], device.NewLogger(device.LogLevelError, "client: "))
	tb.Cleanup(cdev.Close)
	// The in-memory bind sends whatever goes to "port" 2 to the server (see bindtest.NewChannelBinds).
	err = cdev.IpcSet(fmt.Sprintf("private_key=%s\npublic_key=%s\nendpoint=127.0.0.1:2\nallowed_ip=0.0.0.0/0\nallowed_ip=::/0\n",
		clientKey.Hex(), serverKey.Public().Hex()))
	if err != nil {
		tb.Fatal(err)
	}
	if err := cdev.Up(); err != nil {
		tb.Fatal(err)
	}
	return &tunnel{srv: srv, tnet: tnet, client: cdev}
}

// BenchmarkDownlink measures a download through the tunnel over loopback UDP: what a computer sees end to end.
func BenchmarkDownlink(b *testing.B) { benchmarkDownlink(b, newTunnel(b, sourceDialer{}, gateway.Options{})) }

// BenchmarkDownlinkMemory leaves the operating system's UDP out: the tunnel's own cost, both ends together. For
// the gateway's share alone, take a CPU profile and narrow it with -tagfocus side=server.
func BenchmarkDownlinkMemory(b *testing.B) {
	benchmarkDownlink(b, newMemTunnel(b, sourceDialer{}, gateway.Options{}))
}

func benchmarkDownlink(b *testing.B, tn *tunnel) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	c, err := tn.tnet.DialContextTCPAddrPort(ctx, netip.MustParseAddrPort("203.0.113.5:80"))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()

	buf := make([]byte, 1<<20)
	if _, err := io.ReadFull(c, buf); err != nil { // warm up: handshake, window growth
		b.Fatal(err)
	}
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	cpu0 := cpuSeconds()
	for i := 0; i < b.N; i++ {
		if _, err := io.ReadFull(c, buf); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	// Client and server share this process, so this is both ends together.
	b.ReportMetric((cpuSeconds()-cpu0)*1000/float64(b.N), "cpu-ms/MB")
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	b.ReportMetric(float64(mem.Sys)/(1<<20), "sys-MiB") // memory taken from the OS, both ends together

	// Let the stream die before the tunnel is torn down: wireguard-go's client
	// netstack panics ("send on closed channel") if packets still arrive when it
	// is closed. This is the test client's flaw, not the gateway's.
	c.Close()
	for i := 0; i < 250 && tn.srv.Stats().TCPActive != 0; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
}
