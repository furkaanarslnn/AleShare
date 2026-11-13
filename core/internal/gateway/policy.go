package gateway

import (
	"math"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// ---- names nobody gets an address for (Options.Block) ----

// blocked reports whether the query asks for a name that is blocked.
func (g *Gateway) blocked(query []byte) bool {
	if g.opts.Block == nil {
		return false
	}
	var p dnsmessage.Parser
	if _, err := p.Start(query); err != nil {
		return false
	}
	q, err := p.Question()
	return err == nil && g.opts.Block(q.Name.String())
}

// blockedAnswer answers a blocked name the way ad blockers do: 0.0.0.0 for an IPv4 address, :: for an IPv6 one, and
// no records for anything else. Apps take that as "nothing there" at once, without asking again.
func blockedAnswer(query []byte) []byte {
	var p dnsmessage.Parser
	hdr, err := p.Start(query)
	if err != nil {
		return nil
	}
	q, err := p.Question()
	if err != nil {
		return nil
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{
		ID: hdr.ID, Response: true, OpCode: hdr.OpCode, RecursionDesired: hdr.RecursionDesired, RecursionAvailable: true,
	})
	b.StartQuestions()
	b.Question(q)
	b.StartAnswers()
	rh := dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 300}
	switch q.Type {
	case dnsmessage.TypeA:
		b.AResource(rh, dnsmessage.AResource{})
	case dnsmessage.TypeAAAA:
		b.AAAAResource(rh, dnsmessage.AAAAResource{})
	}
	out, err := b.Finish()
	if err != nil {
		return nil
	}
	return out
}

// ---- paused clients (Options.Paused) ----

func (g *Gateway) paused(client netip.Addr) bool {
	return g.opts.Paused != nil && g.opts.Paused(client)
}

// flowSet knows the flows under way per client, to end them when the client is paused.
type flowSet struct {
	mu sync.Mutex
	m  map[netip.Addr]map[*func()]struct{}
	n  int // flows of all clients
}

// full reports whether client, or all the clients together, hold as many flows as they may.
func (f *flowSet) full(client netip.Addr) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.m[client]) >= maxClientFlows || f.n >= maxFlows
}

// add registers a flow of client, ended by stop; the returned function unregisters it.
func (f *flowSet) add(client netip.Addr, stop func()) (remove func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = map[netip.Addr]map[*func()]struct{}{}
	}
	if f.m[client] == nil {
		f.m[client] = map[*func()]struct{}{}
	}
	key := &stop
	f.m[client][key] = struct{}{}
	f.n++
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if _, there := f.m[client][key]; there {
			f.n--
		}
		delete(f.m[client], key)
		if len(f.m[client]) == 0 {
			delete(f.m, client)
		}
	}
}

// cut ends every flow of client and returns how many there were.
func (f *flowSet) cut(client netip.Addr) int {
	f.mu.Lock()
	var stops []func()
	for key := range f.m[client] {
		stops = append(stops, *key)
	}
	f.mu.Unlock()
	for _, stop := range stops {
		stop()
	}
	return len(stops)
}

// CutClient ends the flows under way of a client (a device whose internet was just paused) and returns how many.
func (g *Gateway) CutClient(client netip.Addr) int { return g.flows.cut(client.Unmap()) }

// ForgetClient ends the flows of a client whose device is gone and drops what was counted for its address: the
// next device to get the address starts from nothing.
func (g *Gateway) ForgetClient(client netip.Addr) {
	client = client.Unmap()
	g.flows.cut(client)
	g.usage.mu.Lock()
	delete(g.usage.m, client)
	g.usage.mu.Unlock()
}

// ---- what each client moved over the internet ----

// usage counts, per client address, the bytes its flows carried to and from the internet: what the client costs
// of the mobile data. The tunnel's own headers and what a client exchanges with the phone itself (its services,
// DNS) are not in it, as they are not in Stats.BytesUp and BytesDown, so the clients add up to those.
type usage struct {
	mu sync.Mutex
	m  map[netip.Addr]*atomic.Int64
}

// of returns the counter of client, made on its first flow.
func (u *usage) of(client netip.Addr) *atomic.Int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.m == nil {
		u.m = map[netip.Addr]*atomic.Int64{}
	}
	c := u.m[client]
	if c == nil {
		c = new(atomic.Int64)
		u.m[client] = c
	}
	return c
}

// ClientBytes returns, per client address, the bytes its flows moved to and from the internet since the gateway
// started.
func (g *Gateway) ClientBytes() map[netip.Addr]int64 {
	g.usage.mu.Lock()
	defer g.usage.mu.Unlock()
	out := make(map[netip.Addr]int64, len(g.usage.m))
	for a, c := range g.usage.m {
		out[a] = c.Load()
	}
	return out
}

// ---- smart priority (Options.Priority) ----

const (
	bulkAfter       = 2 << 20         // a flow that moved this much in one direction is a big transfer
	realtimeLinger  = 3 * time.Second // real-time traffic counts as present this long after it was seen
	realtimePackets = 15              // datagrams per second, at least …
	realtimeSize    = 600             // … of at most this many bytes on average
	holdShare       = 0.6             // big transfers get this share of what the link carried before
	minHold         = 128 << 10       // bytes per second big transfers always get
	measureEvery    = time.Second
	// What the link can do shows only while something uses it fully; a quiet minute (some pages, a chat) says
	// nothing about it. So the recent best fades slowly, a hundredth a second, and never below a quarter of the
	// most the link carried since sharing began, which itself fades over tens of minutes (the phone may have moved
	// to a worse network). Fading fast, as it once did, left a download that began during a call after a quiet
	// while at the minimum for the whole call.
	peakFade  = 0.99
	bestFade  = 0.9995
	bestShare = 0.25
)

// priority keeps calls and games smooth on a busy link. The phone's mobile link has deep queues: a big download
// fills them and a call's packets wait behind it. Because the gateway ends the clients' TCP connections itself and
// opens its own towards the internet, it decides how fast it reads a big download (and writes a big upload); reading
// slower makes the sender slow down, and the queues drain. It does so only while a client sends real-time traffic,
// and holds the big transfers to a share of what the link carried just before, so nothing else gets slower.
type priority struct {
	on         func() bool
	realtimeAt atomic.Int64 // UnixNano of the last real-time traffic seen

	mu    sync.Mutex
	tick  time.Time  // the start of the second being measured
	moved [2]int64   // bytes moved in it, per direction
	peak  [2]float64 // bytes per second the link carried lately, per direction
	best  [2]float64 // the most it carried in a second since sharing began (fading slowly)
	limit [2]bucket
}

// Directions of a flow.
const (
	down = 0 // internet to client
	up   = 1 // client to internet
)

func (p *priority) active(now time.Time) bool {
	return p.on != nil && p.on() && now.UnixNano()-p.realtimeAt.Load() < int64(realtimeLinger)
}

// sawRealtime notes a second of a UDP flow's traffic from a client; a steady stream of small datagrams (a call, a
// game) turns the priority on for a while. Port 443 is QUIC: web and video, not real time.
func (p *priority) sawRealtime(packets, bytes int, port uint16, now time.Time) {
	if port == 443 || port == 53 || packets < realtimePackets || bytes/packets > realtimeSize {
		return
	}
	p.realtimeAt.Store(now.UnixNano())
}

// carried counts what a flow moved; once a second the link's recent rate is worked out from the second before.
func (p *priority) carried(dir, n int, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tick.IsZero() {
		p.tick = now
	}
	if elapsed := now.Sub(p.tick); elapsed >= measureEvery {
		p.tick = now
		busy := p.active(now)
		for d := range p.peak {
			rate := float64(p.moved[d]) / elapsed.Seconds()
			p.moved[d] = 0
			if !busy {
				// What the link can do, as the recent best (see peakFade).
				p.best[d] = max(rate, p.best[d]*bestFade)
				p.peak[d] = max(rate, p.peak[d]*peakFade, p.best[d]*bestShare)
			}
			p.limit[d].setRate(max(p.peak[d]*holdShare, minHold), now)
		}
	}
	p.moved[dir] += int64(n)
}

// hold returns how long a big transfer waits after moving n bytes, so that the big transfers together stay within
// their share while real-time traffic is present. Small flows never wait.
func (p *priority) hold(dir int, flowBytes int64, n int, now time.Time) time.Duration {
	if flowBytes < bulkAfter || !p.active(now) {
		return 0
	}
	return p.limit[dir].take(n, now)
}

// bucket is a token bucket: what goes over its rate has to wait.
type bucket struct {
	mu     sync.Mutex
	rate   float64 // bytes per second
	tokens float64
	last   time.Time
}

func (b *bucket) setRate(rate float64, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill(now)
	b.rate = rate
}

func (b *bucket) refill(now time.Time) {
	if !b.last.IsZero() && b.rate > 0 {
		b.tokens = min(b.tokens+now.Sub(b.last).Seconds()*b.rate, b.rate/4) // at most a quarter second of burst
	}
	b.last = now
}

// take spends n bytes and returns how long to wait until the debt is paid.
func (b *bucket) take(n int, now time.Time) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.rate <= 0 {
		return 0
	}
	b.refill(now)
	b.tokens -= float64(n)
	if b.tokens >= 0 {
		return 0
	}
	return time.Duration(-b.tokens / b.rate * float64(time.Second))
}

// spend is take at a given rate: the bucket follows a rate that was changed meanwhile, and 0 lets everything pass.
func (b *bucket) spend(rate float64, n int, now time.Time) time.Duration {
	b.mu.Lock()
	if rate != b.rate {
		b.refill(now)
		b.rate = rate
		b.tokens = min(b.tokens, rate/4)
	}
	b.mu.Unlock()
	return b.take(n, now)
}

// ---- speed limits (Options.Limit) ----

// Limiter holds traffic to a rate, each direction on its own: one is shared by every flow of a device (its IPv4
// and its IPv6 address), another by every client. The rate can change at any time; 0 means no limit.
type Limiter struct {
	rate atomic.Uint64 // math.Float64bits of bytes per second
	dir  [2]bucket
}

// SetRate sets the limit in bytes per second, each way; 0 lifts it.
func (l *Limiter) SetRate(bytesPerSecond float64) {
	l.rate.Store(math.Float64bits(max(bytesPerSecond, 0)))
}

// Rate is the limit in bytes per second; 0 when there is none.
func (l *Limiter) Rate() float64 { return math.Float64frombits(l.rate.Load()) }

func (l *Limiter) wait(dir, n int, now time.Time) time.Duration {
	if l == nil {
		return 0
	}
	rate := l.Rate()
	if rate <= 0 {
		return 0
	}
	return l.dir[dir].spend(rate, n, now)
}

// limitWait returns how long a flow of client waits after moving n bytes in dir: the longest wait of its device's
// limiter and everyone's, so that both stay within their rates.
func (g *Gateway) limitWait(client netip.Addr, dir, n int, now time.Time) time.Duration {
	if g.opts.Limit == nil {
		return 0
	}
	device, everyone := g.opts.Limit(client)
	return max(device.wait(dir, n, now), everyone.wait(dir, n, now))
}

// rtWindow counts one UDP flow's datagrams from the client over one second.
type rtWindow struct {
	start          time.Time
	packets, bytes int
}

// add counts a datagram and, when a second is over, hands the count to see.
func (w *rtWindow) add(n int, now time.Time, see func(packets, bytes int)) {
	if w.start.IsZero() {
		w.start = now
	}
	if now.Sub(w.start) >= time.Second {
		see(w.packets, w.bytes)
		w.start, w.packets, w.bytes = now, 0, 0
	}
	w.packets++
	w.bytes += n
}
