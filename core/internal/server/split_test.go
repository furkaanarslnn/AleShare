package server_test

import (
	"bytes"
	"crypto/tls"
	"io"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"hotspot/core/internal/gateway"
)

// tlsHello is what a real TLS client sends first when it wants to reach host.
func tlsHello(t *testing.T, host string) []byte {
	t.Helper()
	client, server := net.Pipe()
	defer server.Close()
	go func() {
		defer client.Close()
		tls.Client(client, &tls.Config{ServerName: host}).Handshake()
	}()
	server.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 16384)
	n, err := server.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	return buf[:n]
}

// With the splitting on, a handshake still arrives whole and in order at the far end, and what follows it too;
// the gateway counts the flow. With it off, nothing is counted. The switch is looked at for every new flow.
func TestSplitKeepsTheStreamIntact(t *testing.T) {
	var on atomic.Bool
	dialer := &fakeDialer{tcpAddr: startTCPEcho(t), seen: make(chan string, 16)}
	tn := newTunnel(t, dialer, gateway.Options{Split: on.Load})
	hello := tlsHello(t, "discord.com")
	more := bytes.Repeat([]byte("after the handshake "), 200)

	run := func(label string) {
		c, err := tn.tnet.DialTCPAddrPort(netip.MustParseAddrPort("203.0.113.9:443"))
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(10 * time.Second))
		go func() {
			c.Write(hello)
			c.Write(more)
		}()
		got := make([]byte, len(hello)+len(more))
		if _, err := io.ReadFull(c, got); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if !bytes.Equal(got, append(append([]byte(nil), hello...), more...)) {
			t.Fatalf("%s: the echo differs from what was sent", label)
		}
	}

	run("off")
	if n := tn.srv.Stats().TCPSplit; n != 0 {
		t.Fatalf("counted %d splits while off", n)
	}
	on.Store(true)
	run("on")
	if n := tn.srv.Stats().TCPSplit; n != 1 {
		t.Fatalf("counted %d splits while on", n)
	}
	on.Store(false)
	run("off again")
	if n := tn.srv.Stats().TCPSplit; n != 1 {
		t.Fatalf("counted %d splits after switching off", n)
	}
}
