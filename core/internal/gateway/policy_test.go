package gateway

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func query(t *testing.T, name string, typ dnsmessage.Type) []byte {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: 4242, RecursionDesired: true})
	b.StartQuestions()
	b.Question(dnsmessage.Question{Name: dnsmessage.MustNewName(name), Type: typ, Class: dnsmessage.ClassINET})
	q, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return q
}

type countingResolver struct{ asked int }

func (r *countingResolver) Resolve(ctx context.Context, q []byte) ([]byte, bool, error) {
	r.asked++
	return emptyAnswer(q, dnsmessage.RCodeSuccess), true, nil
}

// A blocked name gets 0.0.0.0 or :: (nothing for other types) without the Resolver being asked; others go on.
func TestBlockedNames(t *testing.T) {
	res := &countingResolver{}
	g, err := New(Options{Resolver: res, Block: func(name string) bool { return name == "ads.example.com." }})
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	for typ, want := range map[dnsmessage.Type]int{dnsmessage.TypeA: 1, dnsmessage.TypeAAAA: 1, dnsmessage.TypeTXT: 0} {
		ans := g.answer(query(t, "ads.example.com.", typ))
		var p dnsmessage.Parser
		hdr, err := p.Start(ans)
		if err != nil || hdr.ID != 4242 || !hdr.Response || hdr.RCode != dnsmessage.RCodeSuccess {
			t.Fatalf("%v: header %+v, %v", typ, hdr, err)
		}
		p.SkipAllQuestions()
		answers, _ := p.AllAnswers()
		if len(answers) != want {
			t.Fatalf("%v: %d answers", typ, len(answers))
		}
		for _, a := range answers {
			switch r := a.Body.(type) {
			case *dnsmessage.AResource:
				if r.A != [4]byte{} {
					t.Fatalf("A %v", r.A)
				}
			case *dnsmessage.AAAAResource:
				if r.AAAA != [16]byte{} {
					t.Fatalf("AAAA %v", r.AAAA)
				}
			}
		}
	}
	if res.asked != 0 || g.Stats().DNSBlocked != 3 {
		t.Fatalf("resolver asked %d times, %d counted", res.asked, g.Stats().DNSBlocked)
	}
	g.answer(query(t, "example.com.", dnsmessage.TypeA))
	if res.asked != 1 || g.Stats().DNSBlocked != 3 {
		t.Fatal("a name that is not blocked was not resolved")
	}
}

func TestFlowSetCutsOneClient(t *testing.T) {
	var f flowSet
	a, b := netip.MustParseAddr("10.8.0.2"), netip.MustParseAddr("10.8.0.3")
	var cutA, cutB int
	removeA1 := f.add(a, func() { cutA++ })
	f.add(a, func() { cutA++ })
	f.add(b, func() { cutB++ })
	removeA1()
	if n := f.cut(a); n != 1 || cutA != 1 || cutB != 0 {
		t.Fatalf("cut %d: a %d, b %d", n, cutA, cutB)
	}
	if f.cut(netip.MustParseAddr("10.8.0.9")) != 0 {
		t.Fatal("cut flows of a client that has none")
	}
}

// Real time is a steady stream of small datagrams, not QUIC, not DNS, not a trickle, not big packets.
func TestRealtimeIsSmallSteadyUDP(t *testing.T) {
	now := time.Date(2026, 11, 1, 21, 0, 0, 0, time.UTC)
	on := true
	p := &priority{on: func() bool { return on }}
	for _, c := range []struct {
		packets, bytes int
		port           uint16
	}{{50, 50 * 1200, 3478}, {5, 5 * 160, 3478}, {50, 50 * 160, 443}, {50, 50 * 160, 53}} {
		p.sawRealtime(c.packets, c.bytes, c.port, now)
	}
	if p.active(now) {
		t.Fatal("big packets, a trickle, QUIC or DNS counted as real time")
	}
	p.sawRealtime(50, 50*160, 3478, now) // a call: 50 packets of 160 bytes a second
	if !p.active(now.Add(time.Second)) {
		t.Fatal("a call did not count")
	}
	if p.active(now.Add(realtimeLinger + time.Second)) {
		t.Fatal("still active long after the call")
	}
	on = false
	if p.active(now) {
		t.Fatal("active while switched off")
	}
}

// While a call goes on, big transfers together get a share of what the link carried before; small flows and
// transfers without a call never wait.
func TestPriorityHoldsBigTransfersDuringACall(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 21, 0, 0, 0, time.UTC)
	p := &priority{on: func() bool { return true }}
	// Before the call: the link carries 10 MB/s down.
	now := t0
	for i := 0; i <= 3; i++ {
		p.carried(down, 10<<20, now)
		now = now.Add(time.Second)
	}
	if p.hold(down, bulkAfter*2, 1<<20, now) != 0 {
		t.Fatal("held back without a call")
	}
	p.sawRealtime(50, 50*160, 3478, now)
	p.carried(down, 1, now.Add(time.Second)) // the next measurement sets the hold
	now = now.Add(time.Second)
	if p.hold(down, 64<<10, 1<<20, now) != 0 {
		t.Fatal("a small flow was held back")
	}
	// A big download reads 12 MB, 1 MB at a time, sleeping as told between reads: it gets 60 % of 10 MB/s, after
	// the quarter second of burst the bucket allows.
	var waited time.Duration
	for i := 0; i < 12; i++ {
		w := p.hold(down, bulkAfter*2, 1<<20, now)
		waited += w
		now = now.Add(w)
	}
	rate := (10 << 20) * holdShare
	want := time.Duration((float64(12<<20) - rate/4) / rate * float64(time.Second))
	if waited < want-200*time.Millisecond || waited > want+200*time.Millisecond {
		t.Fatalf("waited %v, want about %v", waited, want)
	}
}

func TestBucketNeverStopsAtZeroRate(t *testing.T) {
	var b bucket
	if b.take(1<<20, time.Now()) != 0 {
		t.Fatal("a bucket without a rate held back")
	}
}

func TestLimiterHoldsToItsRate(t *testing.T) {
	now := time.Now()
	var none *Limiter
	if w := none.wait(down, 1<<20, now); w != 0 {
		t.Fatalf("no limiter, waited %v", w)
	}
	l := new(Limiter)
	if w := l.wait(down, 1<<20, now); w != 0 {
		t.Fatalf("no rate, waited %v", w)
	}
	l.SetRate(1 << 20)
	var waited time.Duration
	for range 10 {
		waited = l.wait(down, 256<<10, now)
	}
	if waited < 2*time.Second || waited > 3*time.Second {
		t.Fatalf("2.5 MiB at once at 1 MiB/s: the last piece waits %v, want about 2.5 s", waited)
	}
	if w := l.wait(up, 256<<10, now); w > 300*time.Millisecond {
		t.Fatalf("the other direction has a bucket of its own, waited %v", w)
	}
	l.SetRate(0)
	if w := l.wait(down, 1<<20, now); w != 0 {
		t.Fatalf("limit lifted, waited %v", w)
	}
}

func TestLimitWaitsForTheSlowerLimiter(t *testing.T) {
	device, everyone := new(Limiter), new(Limiter)
	device.SetRate(1 << 20)
	everyone.SetRate(4 << 20)
	g := &Gateway{opts: Options{Limit: func(netip.Addr) (*Limiter, *Limiter) { return device, everyone }}}
	now := time.Now()
	w := g.limitWait(netip.MustParseAddr("10.8.0.2"), down, 2<<20, now)
	if w < 1500*time.Millisecond || w > 2500*time.Millisecond {
		t.Fatalf("2 MiB at 1 MiB/s for the device: waited %v, want about 2 s", w)
	}
}

// A quiet while before a call must not leave big transfers at the minimum: what the link showed it can do is
// remembered.
func TestPriorityRemembersTheLinkThroughAQuietWhile(t *testing.T) {
	now := time.Date(2026, 11, 1, 21, 0, 0, 0, time.UTC)
	p := &priority{on: func() bool { return true }}
	for i := 0; i <= 3; i++ { // a download showed 10 MB/s
		p.carried(down, 10<<20, now)
		now = now.Add(time.Second)
	}
	for i := 0; i < 10*60; i++ { // ten minutes of next to nothing
		p.carried(down, 2000, now)
		now = now.Add(time.Second)
	}
	p.sawRealtime(50, 50*160, 3478, now)
	now = now.Add(time.Second)
	p.carried(down, 1, now)
	p.limit[down].mu.Lock()
	got := p.limit[down].rate
	p.limit[down].mu.Unlock()
	if got < (10<<20)*bestShare*holdShare*0.7 || got > (10<<20)*holdShare {
		t.Fatalf("big transfers get %.0f bytes a second during the call", got)
	}
}
