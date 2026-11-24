package server_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"hotspot/core/internal/gateway"
)

// A TCP service of the gateway takes connections to the gateway's own address on its port; other ports of that
// address stay closed.
func TestGatewayTCPServiceThroughTheTunnel(t *testing.T) {
	hello := func(c net.Conn) {
		go func() {
			defer c.Close()
			buf := make([]byte, 16)
			n, _ := c.Read(buf)
			c.Write(append([]byte("re:"), buf[:n]...))
		}()
	}
	tn := newTunnel(t, sourceDialer{}, gateway.Options{TCPServices: map[uint16]func(net.Conn){51823: hello}})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := tn.tnet.DialContextTCPAddrPort(ctx, netip.MustParseAddrPort("10.8.0.1:51823"))
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write([]byte("hi"))
	got, err := io.ReadAll(c)
	if err != nil || string(got) != "re:hi" {
		t.Fatalf("got %q, %v", got, err)
	}
	c.Close()
	if c, err := tn.tnet.DialContextTCPAddrPort(ctx, netip.MustParseAddrPort("10.8.0.1:51824")); err == nil {
		c.Close()
		t.Fatal("another port of the gateway's address took a connection")
	}
}

// A paused device loses its internet at once (flows under way end, new ones are refused) and keeps the tunnel:
// the gateway's own services still answer it. Resumed, it is back without doing anything.
func TestPausedClient(t *testing.T) {
	echo := startTCPEcho(t)
	var paused atomic.Bool
	tn := newTunnel(t, &fakeDialer{tcpAddr: echo, seen: make(chan string, 16)}, gateway.Options{
		Paused:   func(netip.Addr) bool { return paused.Load() },
		Services: map[uint16]func([]byte) []byte{51822: func(req []byte) []byte { return req }},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	internet := netip.MustParseAddrPort("203.0.113.5:80")
	c, err := tn.tnet.DialContextTCPAddrPort(ctx, internet)
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write([]byte("ping"))
	if _, err := io.ReadFull(c, make([]byte, 4)); err != nil {
		t.Fatal(err)
	}

	paused.Store(true)
	if n := tn.srv.CutClient(netip.MustParseAddr("10.8.0.2")); n != 1 {
		t.Fatalf("cut %d flows, want 1", n)
	}
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("the flow under way survived the pause")
	}
	if c2, err := tn.tnet.DialContextTCPAddrPort(ctx, internet); err == nil {
		c2.Close()
		t.Fatal("a paused device opened a new flow")
	}
	u, err := tn.tnet.DialUDPAddrPort(netip.AddrPort{}, netip.MustParseAddrPort("10.8.0.1:51822"))
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	u.SetDeadline(time.Now().Add(5 * time.Second))
	u.Write([]byte("hello"))
	if n, err := u.Read(make([]byte, 16)); err != nil || n != 5 {
		t.Fatalf("the gateway's own service did not answer a paused device: %d, %v", n, err)
	}

	paused.Store(false)
	c3, err := tn.tnet.DialContextTCPAddrPort(ctx, internet)
	if err != nil {
		t.Fatalf("resumed: %v", err)
	}
	c3.Close()
}

// A device's count is what it moved over the internet, both ways: not what it exchanged with the phone itself (a
// speed test, a release), which costs no mobile data.
func TestClientBytesCountOnlyTheInternet(t *testing.T) {
	echo := startTCPEcho(t)
	bulk := func(c net.Conn) {
		go func() {
			defer c.Close()
			c.Write(make([]byte, 100<<10))
		}()
	}
	tn := newTunnel(t, &fakeDialer{tcpAddr: echo, seen: make(chan string, 16)}, gateway.Options{
		TCPServices: map[uint16]func(net.Conn){51823: bulk},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	local, err := tn.tnet.DialContextTCPAddrPort(ctx, netip.MustParseAddrPort("10.8.0.1:51823"))
	if err != nil {
		t.Fatal(err)
	}
	local.SetDeadline(time.Now().Add(5 * time.Second))
	if n, _ := io.Copy(io.Discard, local); n != 100<<10 {
		t.Fatalf("the service sent %d bytes", n)
	}
	local.Close()

	c, err := tn.tnet.DialContextTCPAddrPort(ctx, netip.MustParseAddrPort("203.0.113.5:80"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write([]byte("ping"))
	if _, err := io.ReadFull(c, make([]byte, 4)); err != nil {
		t.Fatal(err)
	}
	if got := tn.srv.ClientBytes()[netip.MustParseAddr("10.8.0.2")]; got != 8 {
		t.Fatalf("counted %d bytes for the device, want the 8 of the internet flow", got)
	}
}

// A UDP service of the gateway answers clients on the gateway's own address, and nothing else reaches it.
func TestGatewayServiceAnswersThroughTheTunnel(t *testing.T) {
	echo := func(req []byte) []byte {
		if bytes.Equal(req, []byte("quiet")) {
			return nil
		}
		return append([]byte("re:"), req...)
	}
	tn := newTunnel(t, sourceDialer{}, gateway.Options{Services: map[uint16]func([]byte) []byte{51822: echo}})

	c, err := tn.tnet.DialUDPAddrPort(netip.AddrPort{}, netip.MustParseAddrPort("10.8.0.1:51822"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 64)
	for _, msg := range []string{"one", "two"} {
		if _, err := c.Write([]byte(msg)); err != nil {
			t.Fatal(err)
		}
		n, err := c.Read(buf)
		if err != nil || string(buf[:n]) != "re:"+msg {
			t.Fatalf("got %q, %v", buf[:n], err)
		}
	}
	// No answer is no answer.
	c.Write([]byte("quiet"))
	c.SetDeadline(time.Now().Add(300 * time.Millisecond))
	if n, err := c.Read(buf); err == nil {
		t.Fatalf("answered %q", buf[:n])
	}

	// The tunnel has shaken hands by now and carried the requests, and the server says so.
	if stats := tn.srv.PeerStats(); len(stats) != 1 {
		t.Fatalf("stats %v", stats)
	} else {
		for _, st := range stats {
			if time.Since(st.Handshake) > time.Minute || st.RxBytes == 0 {
				t.Fatalf("stat %+v", st)
			}
		}
	}
}
