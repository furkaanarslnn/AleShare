package doh_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"hotspot/core/internal/doh"
)

// query is a minimal DNS question for example.com A, with ID 0xbeef.
var query = []byte{
	0xbe, 0xef, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0,
	7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0,
	0, 1, 0, 1,
}

// dohServer answers like a DoH server: it checks what RFC 8484 asks for and turns the query into an answer by
// setting the QR bit.
func dohServer(t *testing.T) (*httptest.Server, *tls.Config) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/dns-message" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if body[0] != 0 || body[1] != 0 {
			http.Error(w, "the ID must be 0", http.StatusBadRequest)
			return
		}
		body[2] |= 0x80
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return srv, &tls.Config{RootCAs: pool}
}

// unreachable is an address where nothing listens.
func unreachable(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func TestAnswersOverHTTPSWithTheClientsID(t *testing.T) {
	srv, cfg := dohServer(t)
	addr := strings.TrimPrefix(srv.URL, "https://")
	r := doh.New(nil, []doh.Server{{URL: "https://example.com/dns-query", Addr: addr}}, netip.AddrPort{}, cfg)
	ans, encrypted, err := r.Resolve(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if !encrypted || ans[0] != 0xbe || ans[1] != 0xef || ans[2]&0x80 == 0 {
		t.Fatalf("encrypted=%v answer header % x", encrypted, ans[:4])
	}
}

func TestNextServerWhenOneIsDown(t *testing.T) {
	srv, cfg := dohServer(t)
	addr := strings.TrimPrefix(srv.URL, "https://")
	r := doh.New(nil, []doh.Server{
		{URL: "https://example.com/dns-query", Addr: unreachable(t)},
		{URL: "https://example.com/dns-query", Addr: addr},
	}, netip.AddrPort{}, cfg)
	for i := 0; i < 2; i++ {
		if _, encrypted, err := r.Resolve(context.Background(), query); err != nil || !encrypted {
			t.Fatalf("round %d: encrypted=%v err=%v", i, encrypted, err)
		}
	}
}

// A certificate that does not match must not be accepted: that would be someone in the middle.
func TestRefusesAWrongCertificate(t *testing.T) {
	srv, _ := dohServer(t)
	addr := strings.TrimPrefix(srv.URL, "https://")
	r := doh.New(nil, []doh.Server{{URL: "https://example.com/dns-query", Addr: addr}}, netip.AddrPort{}, nil)
	if _, _, err := r.Resolve(context.Background(), query); err == nil {
		t.Fatal("an untrusted certificate was accepted")
	}
}

// When no DoH server can be reached the plain fallback answers, and says it was plain.
func TestPlainFallbackOnlyWhenHTTPSFails(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go func() {
		buf := make([]byte, 512)
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		buf[2] |= 0x80
		pc.WriteTo(buf[:n], from)
	}()
	fallback := netip.MustParseAddrPort(pc.LocalAddr().String())
	r := doh.New(nil, []doh.Server{{URL: "https://example.com/dns-query", Addr: unreachable(t)}}, fallback, nil)
	ans, encrypted, err := r.Resolve(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if encrypted || ans[0] != 0xbe || ans[1] != 0xef {
		t.Fatalf("encrypted=%v header % x", encrypted, ans[:4])
	}
}

func TestNothingAnswers(t *testing.T) {
	r := doh.New(nil, []doh.Server{{URL: "https://example.com/dns-query", Addr: unreachable(t)}}, netip.AddrPort{}, nil)
	if _, _, err := r.Resolve(context.Background(), query); err == nil {
		t.Fatal("an answer out of nowhere")
	}
	if _, _, err := r.Resolve(context.Background(), []byte{1, 2, 3}); err == nil {
		t.Fatal("a 3-byte message is not a DNS query")
	}
}

// swallowing dials like a network that drops every packet to the DoH servers: a TCP connection never comes about,
// the dial ends only with its context. UDP (the fallback) goes through.
type swallowing struct{}

func (swallowing) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if strings.HasPrefix(network, "tcp") {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

// Servers that never answer must not use up the whole time of a lookup: the fallback still gets asked, and the
// next lookups go to it at once instead of waiting for the servers again.
func TestFallbackWhenTheServersTimeOut(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			buf[2] |= 0x80
			pc.WriteTo(buf[:n], from)
		}
	}()
	servers := []doh.Server{{URL: "https://a.example/dns-query", Addr: "192.0.2.1:443"}, {URL: "https://b.example/dns-query", Addr: "192.0.2.2:443"}, {URL: "https://c.example/dns-query", Addr: "192.0.2.3:443"}}
	r := doh.New(swallowing{}, servers, netip.MustParseAddrPort(pc.LocalAddr().String()), nil)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	began := time.Now()
	ans, encrypted, err := r.Resolve(ctx, query)
	if err != nil {
		t.Fatalf("no answer although the fallback works: %v", err)
	}
	if encrypted || ans[0] != 0xbe || time.Since(began) > 4*time.Second {
		t.Fatalf("encrypted=%v after %v", encrypted, time.Since(began))
	}
	began = time.Now()
	if _, _, err := r.Resolve(context.Background(), query); err != nil || time.Since(began) > time.Second {
		t.Fatalf("the next lookup waited for the servers again: %v, %v", time.Since(began), err)
	}
}
