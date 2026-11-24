package wgcore

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"hotspot/core/internal/provision"
	"hotspot/core/internal/server"
)

type countingDialer struct {
	calls int
	err   error
}

func (d *countingDialer) DialContext(_ context.Context, network, address string) (net.Conn, error) {
	d.calls++
	if d.err != nil {
		return nil, d.err
	}
	a, b := net.Pipe()
	b.Close()
	return a, nil
}

// An uplink without IPv6 is found out once and then remembered for a while.
func TestIPv6Probe(t *testing.T) {
	none := &countingDialer{err: errors.New("connect: network is unreachable")}
	p := &ipv6Probe{dialer: none}
	if p.up() || p.up() {
		t.Fatal("no IPv6 route, yet up")
	}
	if none.calls != 1 {
		t.Fatalf("asked %d times within a minute", none.calls)
	}
	some := &countingDialer{}
	if !(&ipv6Probe{dialer: some}).up() {
		t.Fatal("IPv6 route, yet down")
	}
}

// Each paired device's internet traffic by name, its IPv4 and IPv6 flows together; a device without flows has 0.
func TestPeerTraffic(t *testing.T) {
	peers := []provision.Peer{
		{Name: "Laptop", IPv4: netip.MustParseAddr("10.8.0.2"), IPv6: netip.MustParseAddr("fd42:42:42::2")},
		{Name: "Tablet", IPv4: netip.MustParseAddr("10.8.0.3"), IPv6: netip.MustParseAddr("fd42:42:42::3")},
	}
	used := map[netip.Addr]int64{netip.MustParseAddr("10.8.0.2"): 1000, netip.MustParseAddr("fd42:42:42::2"): 24000}
	if got := peerTraffic(peers, used); got != "Laptop\t25000\nTablet\t0" {
		t.Fatalf("got %q", got)
	}
}

// A device counts while things keep coming from it, and stops counting soon after they stop.
func TestActive(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC)
	window := 75 * time.Second
	var seen rxSeen
	var ok bool

	// Never heard of: not active.
	if seen, ok = active(server.PeerStat{}, seen, t0, window); ok {
		t.Fatal("silent peer is active")
	}
	// Just shook hands: active at once.
	shake := server.PeerStat{Handshake: t0, RxBytes: 148}
	if seen, ok = active(shake, seen, t0.Add(time.Second), window); !ok {
		t.Fatal("fresh handshake not active")
	}
	// Keepalives keep it active long after the handshake.
	if seen, ok = active(server.PeerStat{Handshake: t0, RxBytes: 180}, seen, t0.Add(50*time.Second), window); !ok {
		t.Fatal("peer with keepalives not active")
	}
	if seen, ok = active(server.PeerStat{Handshake: t0, RxBytes: 212}, seen, t0.Add(100*time.Second), window); !ok {
		t.Fatal("peer with keepalives not active later")
	}
	// Nothing more comes: it is gone once the window has passed since the last bytes.
	quiet := server.PeerStat{Handshake: t0, RxBytes: 212}
	if seen, ok = active(quiet, seen, t0.Add(170*time.Second), window); !ok {
		t.Fatal("dropped inside the window")
	}
	if _, ok = active(quiet, seen, t0.Add(180*time.Second), window); ok {
		t.Fatal("quiet peer still active")
	}
}
