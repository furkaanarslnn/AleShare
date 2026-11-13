package wgcore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// steady is a stream that moves 64 KiB every millisecond per stream, until it is told to stop.
func steady(ctx context.Context, count func(int) bool) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
		if !count(64 << 10) {
			return nil
		}
	}
}

func TestMeasureStopsAtTheBudget(t *testing.T) {
	start := time.Now()
	rate, moved, err := measure(context.Background(), 4, 5*time.Second, 8<<20, steady)
	if err != nil {
		t.Fatal(err)
	}
	if moved < 8<<20 || moved > 9<<20 {
		t.Fatalf("moved %d bytes, want the budget of 8 MiB", moved)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("went on after the budget was spent")
	}
	if rate <= 0 {
		t.Fatalf("rate %d", rate)
	}
}

func TestMeasureStopsAtTheTime(t *testing.T) {
	start := time.Now()
	_, moved, err := measure(context.Background(), 4, 300*time.Millisecond, 1<<40, steady)
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %v", d)
	}
	if moved == 0 {
		t.Fatal("nothing moved")
	}
}

func TestMeasureFailsWhenNothingComesThrough(t *testing.T) {
	broken := func(context.Context, func(int) bool) error { return errors.New("no route") }
	if _, _, err := measure(context.Background(), 4, time.Second, 1<<20, broken); err == nil || !strings.Contains(err.Error(), "no route") {
		t.Fatalf("err = %v", err)
	}
}

func TestMeasureCountsBetweenCounts(t *testing.T) {
	// 1 MiB confirmed every 100 ms: 80 Mbit/s or so, whatever the time spent before the first one.
	confirmed := func(ctx context.Context, count func(int) bool) error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(400 * time.Millisecond): // each of the 4 streams: one every 400 ms
			}
			if !count(1 << 20) {
				return nil
			}
		}
	}
	rate, _, err := measure(context.Background(), 4, 3*time.Second, 1<<40, confirmed)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(10 * (1 << 20) * 8); rate < want*7/10 || rate > want*13/10 {
		t.Fatalf("rate %.1f Mbit/s, want about %.1f", float64(rate)/1e6, float64(want)/1e6)
	}
}

func TestUploadCountsWhatTheServerConfirmed(t *testing.T) {
	var got int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := io.Copy(io.Discard, r.Body)
		got += n // one request at a time below
	}))
	defer srv.Close()
	client := srv.Client()
	tr := client.Transport.(*http.Transport)
	tr.TLSClientConfig.ServerName = "example.com" // the test server's certificate
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	var counted int64
	stream := uploadStream(client)
	err := stream(context.Background(), func(n int) bool { counted += int64(n); return counted < 3<<20 })
	if err != nil {
		t.Fatal(err)
	}
	if counted != got || counted < 3<<20 {
		t.Fatalf("counted %d, the server got %d", counted, got)
	}
}

func TestServeSpeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(serveSpeed))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/speed?bytes=300000")
	if err != nil {
		t.Fatal(err)
	}
	n, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if n != 300000 {
		t.Fatalf("got %d bytes", n)
	}
	if resp, _ := http.Get(srv.URL + "/speed?bytes=99999999999"); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("too much asked: %s", resp.Status)
	}
	resp, err = http.Post(srv.URL+"/speed", "application/octet-stream", strings.NewReader(strings.Repeat("x", 12345)))
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Bytes, Ms int64 }
	json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got.Bytes != 12345 {
		t.Fatalf("counted %d bytes", got.Bytes)
	}
}
