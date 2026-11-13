package gateway

import (
	"bytes"
	"crypto/tls"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// clientHello is what a real TLS client sends first when it wants to reach host.
func clientHello(t *testing.T, host string) []byte {
	t.Helper()
	client, server := net.Pipe()
	defer server.Close()
	go func() {
		defer client.Close()
		tls.Client(client, &tls.Config{ServerName: host}).Handshake() // fails: nobody answers
	}()
	server.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 16384)
	n, err := server.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	return buf[:n]
}

func TestSplitPointsOfARealClientHello(t *testing.T) {
	hello := clientHello(t, "discord.com")
	cuts := splitPoints(hello)
	if len(cuts) != 2 || cuts[0] != 1 {
		t.Fatalf("cuts %v of a %d byte hello", cuts, len(hello))
	}
	start, end, ok := serverName(hello)
	if !ok || string(hello[start:end]) != "discord.com" {
		t.Fatalf("name %q, %v", hello[start:end], ok)
	}
	if cuts[1] <= start || cuts[1] >= end {
		t.Fatalf("cut %d is not inside the name [%d,%d)", cuts[1], start, end)
	}
}

func TestSplitPointsLeaveOtherThingsAlone(t *testing.T) {
	for name, b := range map[string][]byte{
		"http":      []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"),
		"empty":     nil,
		"one byte":  {0x16},
		"tls data":  {0x17, 0x03, 0x03, 0x00, 0x05, 1, 2, 3, 4, 5},
		"server hi": {0x16, 0x03, 0x03, 0x00, 0x05, 0x02, 0, 0, 1, 0},
	} {
		if cuts := splitPoints(b); cuts != nil {
			t.Errorf("%s: cuts %v", name, cuts)
		}
	}
}

// A first segment that stops before the name still gets the cut after the first byte, and nothing breaks on the
// way: every prefix of a hello is safe to look at.
func TestSplitPointsOfCutShortHellos(t *testing.T) {
	hello := clientHello(t, "example.org")
	start, _, _ := serverName(hello)
	for n := 0; n <= len(hello); n++ {
		cuts := splitPoints(hello[:n])
		switch {
		case n < 6:
			if cuts != nil {
				t.Fatalf("prefix %d: %v", n, cuts)
			}
		case n < start+len("example.org"):
			if len(cuts) != 1 {
				t.Fatalf("prefix %d before the name: %v", n, cuts)
			}
		default:
			if len(cuts) != 2 || cuts[1] >= n {
				t.Fatalf("prefix %d with the name: %v", n, cuts)
			}
		}
	}
	// Lies about lengths do not run past the end.
	bad := append([]byte(nil), hello...)
	for i := 40; i < 80 && i < len(bad); i++ {
		bad[i] = 0xff
	}
	splitPoints(bad)
}

// recorder keeps what is written to it, one entry per Write.
type recorder struct {
	net.Conn
	writes      [][]byte
	closed      bool
	failAfter   int // fail from this write on; 0 never
	halfClosed  bool
	canHalfShut bool
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.failAfter > 0 && len(r.writes)+1 >= r.failAfter {
		return 0, errors.New("write failed")
	}
	r.writes = append(r.writes, append([]byte(nil), b...))
	return len(b), nil
}

func (r *recorder) Close() error { r.closed = true; return nil }

type halfRecorder struct{ *recorder }

func (h halfRecorder) CloseWrite() error {
	h.halfClosed = true
	h.recorder.halfClosed = true
	return nil
}

func TestSplitConnSplitsOnlyTheFirstWrite(t *testing.T) {
	splitPause = 0
	hello := clientHello(t, "roblox.com")
	var count atomic.Int64
	rec := &recorder{}
	c := &splitConn{Conn: rec, count: &count}

	if n, err := c.Write(hello); err != nil || n != len(hello) {
		t.Fatalf("wrote %d of %d, %v", n, len(hello), err)
	}
	if len(rec.writes) != 3 || len(rec.writes[0]) != 1 {
		t.Fatalf("%d writes, first of %d bytes", len(rec.writes), len(rec.writes[0]))
	}
	if !bytes.Equal(bytes.Join(rec.writes, nil), hello) {
		t.Fatal("the pieces do not make the hello")
	}
	if count.Load() != 1 {
		t.Fatalf("counted %d", count.Load())
	}

	rest := []byte("more bytes after the hello")
	c.Write(rest)
	if len(rec.writes) != 4 || !bytes.Equal(rec.writes[3], rest) || count.Load() != 1 {
		t.Fatalf("later writes were touched: %d writes, counted %d", len(rec.writes), count.Load())
	}
}

func TestSplitConnLeavesOtherFlowsWhole(t *testing.T) {
	var count atomic.Int64
	rec := &recorder{}
	c := &splitConn{Conn: rec, count: &count}
	req := []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
	c.Write(req)
	if len(rec.writes) != 1 || !bytes.Equal(rec.writes[0], req) || count.Load() != 0 {
		t.Fatalf("%d writes, counted %d", len(rec.writes), count.Load())
	}
	// The first write decides: a hello that comes second is not split.
	c.Write(clientHello(t, "example.com"))
	if len(rec.writes) != 2 || count.Load() != 0 {
		t.Fatalf("%d writes, counted %d", len(rec.writes), count.Load())
	}
}

func TestSplitConnReportsAFailedPiece(t *testing.T) {
	splitPause = 0
	hello := clientHello(t, "example.com")
	rec := &recorder{failAfter: 2}
	n, err := (&splitConn{Conn: rec, count: new(atomic.Int64)}).Write(hello)
	if err == nil || n != 1 {
		t.Fatalf("wrote %d, %v", n, err)
	}
}

func TestSplitConnCloseWrite(t *testing.T) {
	// A connection that can half-close keeps doing so.
	rec := &recorder{}
	if err := (&splitConn{Conn: halfRecorder{rec}}).CloseWrite(); err != nil || !rec.halfClosed || rec.closed {
		t.Fatalf("half close: %v, half-closed %v, closed %v", err, rec.halfClosed, rec.closed)
	}
	// One that cannot is closed whole, as the relay does without a CloseWrite.
	rec = &recorder{}
	if err := (&splitConn{Conn: rec}).CloseWrite(); err != nil || !rec.closed {
		t.Fatalf("close: %v, closed %v", err, rec.closed)
	}
}
