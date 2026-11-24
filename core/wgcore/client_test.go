package wgcore

import (
	"testing"

	"hotspot/core/internal/provision"
)

// The app builds its VPN from the plain strings of a Join, so those must come out right.
func TestJoinFieldsForTheApp(t *testing.T) {
	state := provision.NewState()
	p, priv, err := state.AddPeer("tablet")
	if err != nil {
		t.Fatal(err)
	}
	conf := provision.ClientConfig(priv, p, state.ServerPublicKey(), "192.168.49.1:51820")

	link, err := EncodeJoin("DIRECT-HS-Q4TZ", "abcdEFGH2345jkmn", conf)
	if err != nil {
		t.Fatal(err)
	}
	j, err := ParseJoin(link)
	if err != nil {
		t.Fatal(err)
	}
	want := Join{
		SSID: "DIRECT-HS-Q4TZ", Password: "abcdEFGH2345jkmn", Config: conf, Endpoint: "192.168.49.1:51820",
		Addresses: p.IPv4.String() + "/32," + p.IPv6.String() + "/128",
		DNS:       "10.8.0.1,fd42:42:42::1",
		Routes:    "0.0.0.0/0,::/0",
		MTU:       provision.TunnelMTU,
	}
	if *j != want {
		t.Fatalf("got  %+v\nwant %+v", *j, want)
	}
	if _, err := ParseJoin("hotspot://join?d=bad"); err == nil {
		t.Fatal("a damaged link must be refused")
	}
}
