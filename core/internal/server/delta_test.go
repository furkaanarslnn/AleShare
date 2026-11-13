package server

import (
	"net/netip"
	"strings"
	"testing"

	"hotspot/core/internal/wgkey"
)

// Adding or removing a device must leave the other devices' sessions alone: only what changed is sent.
func TestPeersDelta(t *testing.T) {
	peer := func(n byte) Peer {
		var k wgkey.Key
		k[0] = n
		return Peer{PublicKey: k, IPv4: netip.AddrFrom4([4]byte{10, 8, 0, n})}
	}
	a, b, c := peer(2), peer(3), peer(4)
	old := byKey([]Peer{a, b})

	if d := peersDelta(old, []Peer{b, a}); d != "" {
		t.Fatalf("nothing changed, yet %q is sent", d)
	}
	d := peersDelta(old, []Peer{a, b, c})
	if want := "public_key=" + c.PublicKey.Hex() + "\nallowed_ip=10.8.0.4/32\n"; d != want {
		t.Fatalf("adding a device sends %q, want %q", d, want)
	}
	if strings.Contains(d, "replace_peers") {
		t.Fatal("the whole set is replaced")
	}
	d = peersDelta(old, []Peer{a})
	if want := "public_key=" + b.PublicKey.Hex() + "\nremove=true\n"; d != want {
		t.Fatalf("removing a device sends %q, want %q", d, want)
	}
	moved := a
	moved.IPv4 = netip.AddrFrom4([4]byte{10, 8, 0, 9})
	d = peersDelta(old, []Peer{moved, b})
	if want := "public_key=" + a.PublicKey.Hex() + "\nremove=true\npublic_key=" + a.PublicKey.Hex() + "\nallowed_ip=10.8.0.9/32\n"; d != want {
		t.Fatalf("a device with a new address sends %q, want %q", d, want)
	}
}
