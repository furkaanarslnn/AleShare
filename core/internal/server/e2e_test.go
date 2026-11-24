package server_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"

	"hotspot/core/internal/gateway"
	"hotspot/core/internal/server"
	"hotspot/core/internal/wgkey"
)

// fakeDialer stands in for the internet: whatever the gateway wants to reach,
// it is connected to a local echo server, and the requested target is recorded.
type fakeDialer struct {
	tcpAddr, udpAddr string
	seen             chan string
}

func (d *fakeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	select {
	case d.seen <- network + " " + address:
	default:
	}
	switch network {
	case "tcp":
		return (&net.Dialer{}).DialContext(ctx, "tcp", d.tcpAddr)
	case "udp":
		return (&net.Dialer{}).DialContext(ctx, "udp", d.udpAddr)
	}
	return nil, errors.New("unsupported network " + network)
}

func (d *fakeDialer) expect(t *testing.T, want string) {
	t.Helper()
	select {
	case got := <-d.seen:
		if got != want {
			t.Fatalf("gateway dialed %q, want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("gateway never dialed %q", want)
	}
}

func startTCPEcho(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	return l.Addr().String()
}

func startUDPEcho(t *testing.T) string {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, addr, err := c.ReadFrom(buf)
			if err != nil {
				return
			}
			c.WriteTo(buf[:n], addr)
		}
	}()
	return c.LocalAddr().String()
}

func freeUDPPort(t *testing.T) uint16 {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return uint16(c.LocalAddr().(*net.UDPAddr).Port)
}

func TestEndToEnd(t *testing.T) {
	dialer := &fakeDialer{
		tcpAddr: startTCPEcho(t),
		udpAddr: startUDPEcho(t),
		seen:    make(chan string, 16),
	}

	var (
		gw4        = netip.MustParseAddr("10.8.0.1")
		gw6        = netip.MustParseAddr("fd42:42:42::1")
		client4    = netip.MustParseAddr("10.8.0.2")
		client6    = netip.MustParseAddr("fd42:42:42::2")
		serverKey  = wgkey.Generate()
		clientKey  = wgkey.Generate()
		psk        = wgkey.Random()
		serverPort = freeUDPPort(t)
	)

	srv, err := server.Start(server.Config{
		PrivateKey: serverKey,
		ListenPort: serverPort,
		Peers:      []server.Peer{{PublicKey: clientKey.Public(), PresharedKey: psk, IPv4: client4, IPv6: client6}},
		Gateway: gateway.Options{
			Addrs:       []netip.Addr{gw4, gw6},
			Dialer:      dialer,
			DNSUpstream: netip.MustParseAddrPort("9.9.9.9:53"),
		},
		LogLevel: device.LogLevelError,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	// The client is an in-process WireGuard peer with its own user-space stack.
	ctun, tnet, err := netstack.CreateNetTUN([]netip.Addr{client4, client6}, []netip.Addr{gw4}, 1420)
	if err != nil {
		t.Fatal(err)
	}
	cdev := device.NewDevice(ctun, conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, "client: "))
	defer cdev.Close()
	err = cdev.IpcSet(fmt.Sprintf("private_key=%s\npublic_key=%s\npreshared_key=%s\nendpoint=127.0.0.1:%d\nallowed_ip=0.0.0.0/0\nallowed_ip=::/0\n",
		clientKey.Hex(), serverKey.Public().Hex(), psk.Hex(), serverPort))
	if err != nil {
		t.Fatal(err)
	}
	if err := cdev.Up(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	t.Run("tcp", func(t *testing.T) {
		c, err := tnet.DialContextTCPAddrPort(ctx, netip.MustParseAddrPort("203.0.113.5:80"))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err := c.Write([]byte("hello through the tunnel")); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, len("hello through the tunnel"))
		if _, err := io.ReadFull(c, buf); err != nil {
			t.Fatal(err)
		}
		if string(buf) != "hello through the tunnel" {
			t.Fatalf("echo = %q", buf)
		}
		dialer.expect(t, "tcp 203.0.113.5:80")
	})

	t.Run("tcp large transfer", func(t *testing.T) {
		c, err := tnet.DialContextTCPAddrPort(ctx, netip.MustParseAddrPort("203.0.113.6:443"))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(15 * time.Second))
		payload := []byte(strings.Repeat("0123456789abcdef", 64<<10)) // 1 MiB
		go func() {
			c.Write(payload)
		}()
		got := make([]byte, len(payload))
		if _, err := io.ReadFull(c, got); err != nil {
			t.Fatal(err)
		}
		if string(got) != string(payload) {
			t.Fatal("payload corrupted")
		}
		dialer.expect(t, "tcp 203.0.113.6:443")
	})

	t.Run("udp", func(t *testing.T) {
		u, err := tnet.DialUDPAddrPort(netip.AddrPort{}, netip.MustParseAddrPort("198.51.100.7:4433"))
		if err != nil {
			t.Fatal(err)
		}
		defer u.Close()
		u.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err := u.Write([]byte("datagram")); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 64)
		n, err := u.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		if string(buf[:n]) != "datagram" {
			t.Fatalf("echo = %q", buf[:n])
		}
		dialer.expect(t, "udp 198.51.100.7:4433")
	})

	t.Run("dns goes to the configured upstream", func(t *testing.T) {
		u, err := tnet.DialUDPAddrPort(netip.AddrPort{}, netip.AddrPortFrom(gw4, 53))
		if err != nil {
			t.Fatal(err)
		}
		defer u.Close()
		u.SetDeadline(time.Now().Add(10 * time.Second))
		if _, err := u.Write([]byte("who is example.org")); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 64)
		n, err := u.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		if string(buf[:n]) != "who is example.org" {
			t.Fatalf("echo = %q", buf[:n])
		}
		dialer.expect(t, "udp 9.9.9.9:53")
	})

	t.Run("private addresses are refused", func(t *testing.T) {
		dctx, dcancel := context.WithTimeout(ctx, 6*time.Second)
		defer dcancel()
		start := time.Now()
		c, err := tnet.DialContextTCPAddrPort(dctx, netip.MustParseAddrPort("192.168.1.10:80"))
		if err == nil {
			c.Close()
			t.Fatal("connection to a private address was allowed")
		}
		if time.Since(start) > 4*time.Second {
			t.Fatalf("refusal took %v, want an immediate RST (err: %v)", time.Since(start), err)
		}
		select {
		case got := <-dialer.seen:
			t.Fatalf("gateway dialed %q for a private address", got)
		default:
		}
	})

	if st := srv.Stats(); st.TCPTotal < 2 || st.UDPTotal < 2 || st.BytesUp == 0 || st.BytesDown == 0 {
		t.Fatalf("unexpected stats: %+v", st)
	}
}
