package wgcore

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"hotspot/core/internal/doh"
	"hotspot/core/internal/gateway"
)

// The speed test measures the phone's own way to the internet (the mobile network while sharing runs): how fast
// it downloads and uploads, and its round trip. It uses Cloudflare's speed test service, as speed.cloudflare.com
// does, over several connections at once (a mobile link shows its speed only that way), and stops at a budget of
// data, since mobile data costs money.
const (
	speedHost   = "speed.cloudflare.com"
	downStreams = 4           // a mobile link shows its download speed only over several connections
	upStreams   = 2           // fewer for uploads: each request waits for its answer, and fewer streams make bigger requests
	speedWarmup = time.Second // TCP's slow start: the first second is left out of the rate
	downTime    = 8 * time.Second
	upTime      = 6 * time.Second
	downBudget  = 40 << 20
	upBudget    = 16 << 20
	speedChunk  = 8 << 20 // bytes asked for in one download request
)

// outbound is how sharing reaches the internet while it runs: its sockets are bound to the mobile network.
type outbound struct {
	dialer    gateway.Dialer
	dohDialer doh.Dialer
}

// speedResult is what a speed test found; rates in bits per second.
type speedResult struct {
	Down  int64 `json:"down_bps"`
	Up    int64 `json:"up_bps"`
	Ping  int64 `json:"ping_ms"`
	Bytes int64 `json:"bytes"` // the data the test used, both ways
	At    int64 `json:"at"`    // Unix seconds
}

// SpeedTest measures the phone's internet (see above) and returns the result as JSON: down_bps, up_bps, ping_ms,
// bytes (the data it used) and at. It takes about 20 seconds; another test asked for meanwhile fails at once.
func (c *Core) SpeedTest() (string, error) {
	if !c.speed.TryLock() {
		return "", errors.New("wgcore: a speed test is running")
	}
	defer c.speed.Unlock()
	c.mu.Lock()
	out, dns := c.out, c.dns
	c.mu.Unlock()
	dialer := out.dialer
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	client := speedClient(dialer, doh.New(out.dohDialer, doh.DefaultServers, dns, nil))
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	ping, err := measurePing(ctx, client)
	if err != nil {
		return "", fmt.Errorf("wgcore: speed test: %w", err)
	}
	down, downBytes, err := measure(ctx, downStreams, downTime, downBudget, downloadStream(client))
	if err != nil {
		return "", fmt.Errorf("wgcore: speed test, download: %w", err)
	}
	up, upBytes, err := measure(ctx, upStreams, upTime, upBudget, uploadStream(client))
	if err != nil {
		return "", fmt.Errorf("wgcore: speed test, upload: %w", err)
	}
	b, _ := json.Marshal(speedResult{Down: down, Up: up, Ping: ping.Milliseconds(), Bytes: downBytes + upBytes, At: time.Now().Unix()})
	return string(b), nil
}

// measure runs streams copies of stream until d passed or budget bytes moved, and returns the rate in bits
// per second and the bytes moved. A stream calls count with the bytes that arrived (or that the server confirmed
// it got), and stops when told to: the budget is spent. The rate is what was counted between the first count after
// the warm-up and the last one, when there was time for that; else from the start to the last count.
func measure(ctx context.Context, streams int, d time.Duration, budget int64, stream func(ctx context.Context, count func(int) bool) error) (rate, moved int64, err error) {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	type mark struct {
		at    time.Time
		bytes int64
	}
	start := time.Now()
	var (
		mu         sync.Mutex
		total      int64
		warm, last mark
	)
	count := func(n int) bool {
		mu.Lock()
		total += int64(n)
		now := mark{time.Now(), total}
		if warm.at.IsZero() && now.at.Sub(start) >= speedWarmup {
			// What was counted up to now belongs to the warm-up, this count too: for an upload it is a whole
			// request, megabytes that left before the mark, and counting it made uploads look faster than they are.
			warm = mark{now.at, total}
		}
		last = now
		over := total >= budget
		mu.Unlock()
		if over {
			cancel()
			return false
		}
		return true
	}
	done := make(chan error, streams)
	for range streams {
		go func() { done <- stream(ctx, count) }()
	}
	var firstErr error
	for range streams {
		// A stream ended by the time or the budget is no failure.
		if err := <-done; err != nil && ctx.Err() == nil && firstErr == nil {
			firstErr = err
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if total == 0 {
		if firstErr == nil {
			firstErr = errors.New("nothing came through")
		}
		return 0, 0, firstErr
	}
	from := mark{start, 0}
	if !warm.at.IsZero() && last.at.Sub(warm.at) >= d/4 {
		from = warm
	}
	elapsed := last.at.Sub(from.at).Seconds()
	if elapsed <= 0 {
		elapsed = time.Since(start).Seconds()
	}
	return int64(float64(last.bytes-from.bytes) * 8 / elapsed), total, nil
}

// measurePing returns the shortest of a few request round trips over one connection; the first one, which sets
// the connection up, does not count.
func measurePing(ctx context.Context, client *http.Client) (time.Duration, error) {
	var best time.Duration
	for i := range 4 {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+speedHost+"/__down?bytes=0", nil)
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return 0, err
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if d := time.Since(start); i > 0 && (best == 0 || d < best) {
			best = d
		}
	}
	return best, nil
}

func downloadStream(client *http.Client) func(context.Context, func(int) bool) error {
	return func(ctx context.Context, count func(int) bool) error {
		buf := make([]byte, 32<<10)
		for {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+speedHost+"/__down?bytes="+strconv.Itoa(speedChunk), nil)
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				return fmt.Errorf("download: %s", resp.Status)
			}
			for {
				n, err := resp.Body.Read(buf)
				if n > 0 && !count(n) {
					resp.Body.Close()
					return nil
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					resp.Body.Close()
					return err
				}
			}
			resp.Body.Close()
		}
	}
}

// uploadStream sends requests of zeros. What the phone hands its socket is not sent yet (the kernel holds megabytes
// of it), so a request counts once the server answered it, that is once it got all of it. The requests start small,
// for slow links, and grow while they finish quickly, so that waiting for the answers costs little on fast ones.
// The streams of one measurement share the budget: a request is not started when it would go over it.
func uploadStream(client *http.Client) func(context.Context, func(int) bool) error {
	var reserved atomic.Int64
	return func(ctx context.Context, count func(int) bool) error {
		size := upFirst
		for {
			if reserved.Add(int64(size)) > upBudget {
				return nil
			}
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+speedHost+"/__up", &zeros{left: size})
			req.ContentLength = int64(size)
			start := time.Now()
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("upload: %s", resp.Status)
			}
			if !count(size) {
				return nil
			}
			if time.Since(start) < upGrowUnder && size < upLargest {
				size *= 2
			}
		}
	}
}

const (
	upFirst     = 256 << 10
	upLargest   = 8 << 20
	upGrowUnder = time.Second
)

// zeros is a request body of left zero bytes.
type zeros struct{ left int }

func (z *zeros) Read(p []byte) (int, error) {
	if z.left <= 0 {
		return 0, io.EOF
	}
	n := min(len(p), z.left)
	clear(p[:n])
	z.left -= n
	return n, nil
}

// speedClient reaches the speed test the way sharing reaches the internet: names over DoH, sockets from dialer,
// and one TCP connection per stream (no HTTP/2, which would put the streams on one).
func speedClient(dialer gateway.Dialer, resolver *doh.Resolver) *http.Client {
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := lookup4(ctx, resolver, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				var conn net.Conn
				if conn, err = dialer.DialContext(ctx, "tcp4", net.JoinHostPort(ip.String(), port)); err == nil {
					return conn, nil
				}
			}
			return nil, err
		},
		TLSHandshakeTimeout: 10 * time.Second,
		TLSNextProto:        map[string]func(string, *tls.Conn) http.RoundTripper{},
		MaxIdleConnsPerHost: downStreams,
		DisableCompression:  true,
	}}
}

// lookup4 returns the IPv4 addresses of host, asked over DoH.
func lookup4(ctx context.Context, r *doh.Resolver, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip}, nil
	}
	name, err := dnsmessage.NewName(host + ".")
	if err != nil {
		return nil, err
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: uint16(rand.Uint32()), RecursionDesired: true})
	b.StartQuestions()
	b.Question(dnsmessage.Question{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET})
	query, err := b.Finish()
	if err != nil {
		return nil, err
	}
	answer, _, err := r.Resolve(ctx, query)
	if err != nil {
		return nil, err
	}
	var p dnsmessage.Parser
	if _, err := p.Start(answer); err != nil {
		return nil, err
	}
	p.SkipAllQuestions()
	var ips []netip.Addr
	for {
		h, err := p.AnswerHeader()
		if err != nil {
			break
		}
		if h.Type != dnsmessage.TypeA {
			p.SkipAnswer()
			continue
		}
		if a, err := p.AResource(); err == nil {
			ips = append(ips, netip.AddrFrom4(a.A))
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no address for %s", host)
	}
	return ips, nil
}

// ---- the Wi-Fi side, for a paired computer (GET and POST /speed on the store, see update.go) ----

const maxSpeedBytes = 512 << 20

// serveSpeed sends the bytes a computer asks for (zeros: the tunnel encrypts them anyway), or takes what it sends
// and says how much came in how long. Nothing of it leaves the phone: it measures the Wi-Fi and the tunnel.
func serveSpeed(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		start := time.Now()
		n, _ := io.Copy(io.Discard, io.LimitReader(r.Body, maxSpeedBytes))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"bytes":%d,"ms":%d}`, n, time.Since(start).Milliseconds())
		return
	}
	n, err := strconv.ParseInt(r.URL.Query().Get("bytes"), 10, 64)
	if err != nil || n <= 0 || n > maxSpeedBytes {
		http.Error(w, "bytes", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(n, 10))
	buf := make([]byte, 64<<10)
	for n > 0 {
		m, err := w.Write(buf[:min(int64(len(buf)), n)])
		if err != nil {
			return
		}
		n -= int64(m)
	}
}
