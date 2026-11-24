package wifiinfo

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"hotspot/core/internal/wgkey"
)

var (
	current = Network{SSID: "DIRECT-HS-Q4TZ", Password: "abcdEFGH2345jkmn"}
	next    = Network{SSID: "DIRECT-HS-Ev", Password: "yeni-parola-123"}
)

// phone starts a responder on a loopback UDP socket and returns its address.
func phone(t *testing.T, r *Responder, accept func(netip.Addr) bool) net.Addr {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go r.Serve(conn, accept)
	return conn.LocalAddr()
}

func ask(t *testing.T, addr net.Addr, private, phonePub wgkey.Key, wait time.Duration) (Info, error) {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	return Ask(ctx, conn, addr, private, phonePub)
}

func TestAskAndLearn(t *testing.T) {
	server, device := wgkey.Generate(), wgkey.Generate()
	r := NewResponder(server)
	r.SetPeers([]wgkey.Key{device.Public()})
	r.SetInfo(&Info{Current: current})
	addr := phone(t, r, nil)

	info, err := ask(t, addr, device, server.Public(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if info.Current != current || info.Next != nil {
		t.Fatalf("got %+v", info)
	}
	if len(r.Learned()) != 0 {
		t.Fatal("learned without a Next")
	}
	if a := r.Asked(); len(a) != 1 || a[0] != device.Public() {
		t.Fatalf("asked %v", a)
	}

	r.SetInfo(&Info{Current: current, Next: &next})
	info, err = ask(t, addr, device, server.Public(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if info.Next == nil || *info.Next != next {
		t.Fatalf("got %+v", info)
	}
	if l := r.Learned(); len(l) != 1 || l[0] != device.Public() {
		t.Fatalf("learned %v", l)
	}

	// Another network to follow: nobody knows it yet.
	other := Network{SSID: "DIRECT-HS-Is", Password: "baska-parola-9"}
	r.SetInfo(&Info{Current: current, Next: &other})
	if len(r.Learned()) != 0 {
		t.Fatal("learned survived a new Next")
	}
}

func TestStrangersGetNothing(t *testing.T) {
	server, device, stranger := wgkey.Generate(), wgkey.Generate(), wgkey.Generate()
	r := NewResponder(server)
	r.SetPeers([]wgkey.Key{device.Public()})
	r.SetInfo(&Info{Current: current})
	addr := phone(t, r, nil)

	if _, err := ask(t, addr, stranger, server.Public(), 800*time.Millisecond); !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("stranger: %v", err)
	}
	// A device that believes in another phone (a network that is not ours) cannot read our answers either.
	if _, err := ask(t, addr, device, wgkey.Generate().Public(), 800*time.Millisecond); !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("wrong phone: %v", err)
	}
	// A removed device is a stranger.
	r.SetPeers(nil)
	if _, err := ask(t, addr, device, server.Public(), 800*time.Millisecond); !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("removed device: %v", err)
	}
}

func TestNothingToTell(t *testing.T) {
	server, device := wgkey.Generate(), wgkey.Generate()
	r := NewResponder(server)
	r.SetPeers([]wgkey.Key{device.Public()})
	addr := phone(t, r, nil)
	if _, err := ask(t, addr, device, server.Public(), 800*time.Millisecond); !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("no info: %v", err)
	}
}

func TestAcceptFilter(t *testing.T) {
	server, device := wgkey.Generate(), wgkey.Generate()
	r := NewResponder(server)
	r.SetPeers([]wgkey.Key{device.Public()})
	r.SetInfo(&Info{Current: current})
	addr := phone(t, r, func(a netip.Addr) bool { return a == netip.MustParseAddr("192.168.49.2") })
	if _, err := ask(t, addr, device, server.Public(), 800*time.Millisecond); !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("filtered sender: %v", err)
	}
}

// An answer belongs to its challenge: one recorded earlier does not pass for a new request.
func TestNoReplay(t *testing.T) {
	server, device := wgkey.Generate(), wgkey.Generate()
	r := NewResponder(server)
	r.SetPeers([]wgkey.Key{device.Public()})
	r.SetInfo(&Info{Current: current})
	k, err := pairKey(device, server.Public())
	if err != nil {
		t.Fatal(err)
	}
	c1 := []byte("0123456789abcdef")
	c2 := []byte("fedcba9876543210")
	answer := r.Answer(append(append([]byte(magic), c1...), requestTag(k, c1)...))
	if answer == nil {
		t.Fatal("no answer")
	}
	if _, ok := open(answerAEAD(k, c1), c1, answer); !ok {
		t.Fatal("own answer rejected")
	}
	if _, ok := open(answerAEAD(k, c2), c2, answer); ok {
		t.Fatal("replayed answer accepted")
	}
	// A tag that does not fit its challenge gets nothing.
	if r.Answer(append(append([]byte(magic), c2...), requestTag(k, c1)...)) != nil {
		t.Fatal("answered a forged request")
	}
}

func TestRateLimit(t *testing.T) {
	server, device := wgkey.Generate(), wgkey.Generate()
	r := NewResponder(server)
	r.SetPeers([]wgkey.Key{device.Public()})
	r.SetInfo(&Info{Current: current})
	k, _ := pairKey(device, server.Public())
	c := []byte("0123456789abcdef")
	req := append(append([]byte(magic), c...), requestTag(k, c)...)
	answered := 0
	for i := 0; i < 3*burst; i++ {
		if r.Answer(req) != nil {
			answered++
		}
	}
	if answered < burst || answered > burst+2 {
		t.Fatalf("answered %d of %d at once", answered, 3*burst)
	}
}

func TestValid(t *testing.T) {
	for _, n := range []Network{current, next} {
		if !n.Valid() {
			t.Errorf("%q rejected", n.SSID)
		}
	}
	for _, n := range []Network{
		{SSID: "Ev", Password: "abcdefgh"},
		{SSID: "DIRECT-HS-x", Password: "short"},
		{SSID: "DIRECT-HS-0123456789012345678901234", Password: "abcdefgh"},
	} {
		if n.Valid() {
			t.Errorf("%+v accepted", n)
		}
	}
}

// Requests from nobody paired, however many, take nothing from what a paired device may ask; and one device's
// requests take nothing from another's.
func TestStrangersDoNotUseTheAllowanceUp(t *testing.T) {
	server, device, other, stranger := wgkey.Generate(), wgkey.Generate(), wgkey.Generate(), wgkey.Generate()
	r := NewResponder(server)
	r.SetPeers([]wgkey.Key{device.Public(), other.Public()})
	r.SetInfo(&Info{Current: current})
	c := []byte("0123456789abcdef")
	request := func(private wgkey.Key) []byte {
		k, _ := pairKey(private, server.Public())
		return append(append([]byte(magic), c...), requestTag(k, c)...)
	}
	for i := 0; i < 50*burst; i++ {
		if r.Answer(request(stranger)) != nil {
			t.Fatal("a stranger got an answer")
		}
	}
	for i := 0; i < 3*burst; i++ {
		r.Answer(request(other))
	}
	if r.Answer(request(device)) == nil {
		t.Fatal("a paired device got no answer after others asked a lot")
	}
}

// A device named with SetDevices is told its own token, a computer the other one.
func TestDevicesGetTheirOwnToken(t *testing.T) {
	server, computer, tablet := wgkey.Generate(), wgkey.Generate(), wgkey.Generate()
	r := NewResponder(server)
	r.SetDevices([]wgkey.Key{tablet.Public()})
	r.SetPeers([]wgkey.Key{computer.Public(), tablet.Public()})
	r.SetInfo(&Info{Current: current, App: &App{Code: 41, Name: "1.2.1", Token: "full", DeviceToken: "less"}, PC: &App{Code: 41, Token: "full", DeviceToken: "less"}})
	addr := phone(t, r, nil)
	for name, c := range map[string]struct {
		key  wgkey.Key
		want string
	}{"computer": {computer, "full"}, "tablet": {tablet, "less"}} {
		info, err := ask(t, addr, c.key, server.Public(), 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if info.App == nil || info.App.Token != c.want || info.PC == nil || info.PC.Token != c.want || info.App.Code != 41 {
			t.Errorf("%s was told %+v, %+v", name, info.App, info.PC)
		}
	}
	// A phone that made no token for devices gives them none, never the computers'.
	r.SetInfo(&Info{Current: current, App: &App{Code: 41, Token: "full"}})
	if info, err := ask(t, addr, tablet, server.Public(), 2*time.Second); err != nil || info.App.Token != "" {
		t.Fatalf("tablet without a device token: %+v, %v", info.App, err)
	}
}
