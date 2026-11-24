package server_test

import (
	"context"
	"errors"
	"io"
	"net/netip"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"hotspot/core/internal/gateway"
)

// fakeResolver answers every question with n A records, or fails.
type fakeResolver struct {
	n    int
	fail bool
}

func (f fakeResolver) Resolve(_ context.Context, query []byte) ([]byte, bool, error) {
	if f.fail {
		return nil, false, errors.New("no server")
	}
	var p dnsmessage.Parser
	hdr, err := p.Start(query)
	if err != nil {
		return nil, false, err
	}
	q, err := p.Question()
	if err != nil {
		return nil, false, err
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: hdr.ID, Response: true, RecursionAvailable: true})
	b.StartQuestions()
	b.Question(q)
	b.StartAnswers()
	for i := 0; i < f.n; i++ {
		b.AResource(dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 60}, dnsmessage.AResource{A: [4]byte{192, 0, 2, byte(i)}})
	}
	ans, err := b.Finish()
	return ans, true, err
}

func dnsQuery(t *testing.T, id uint16, ednsSize uint16) []byte {
	t.Helper()
	return dnsQueryOf(t, id, ednsSize, dnsmessage.TypeA)
}

func dnsQueryOf(t *testing.T, id uint16, ednsSize uint16, qtype dnsmessage.Type) []byte {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: id, RecursionDesired: true})
	b.StartQuestions()
	b.Question(dnsmessage.Question{Name: dnsmessage.MustNewName("example.com."), Type: qtype, Class: dnsmessage.ClassINET})
	if ednsSize > 0 {
		b.StartAdditionals()
		var rh dnsmessage.ResourceHeader
		rh.SetEDNS0(int(ednsSize), dnsmessage.RCodeSuccess, false)
		b.OPTResource(rh, dnsmessage.OPTResource{})
	}
	q, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func readHeader(t *testing.T, msg []byte) (dnsmessage.Header, int) {
	t.Helper()
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil {
		t.Fatalf("not a DNS message: %v", err)
	}
	p.SkipAllQuestions()
	answers, _ := p.AllAnswers()
	return h, len(answers)
}

var dnsServer = netip.MustParseAddrPort("10.8.0.1:53")

func udpExchange(t *testing.T, tn *tunnel, q []byte) []byte {
	t.Helper()
	return udpExchangeWith(t, tn, dnsServer, q)
}

func udpExchangeWith(t *testing.T, tn *tunnel, server netip.AddrPort, q []byte) []byte {
	t.Helper()
	c, err := tn.tnet.DialUDPAddrPort(netip.AddrPort{}, server)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write(q); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	return buf[:n]
}

// The gateway answers DNS itself, through the Resolver, and counts encrypted answers.
func TestDNSOverUDPGoesThroughTheResolver(t *testing.T) {
	tn := newTunnel(t, sourceDialer{}, gateway.Options{Resolver: fakeResolver{n: 2}})
	h, n := readHeader(t, udpExchange(t, tn, dnsQuery(t, 0x1234, 0)))
	if h.ID != 0x1234 || !h.Response || n != 2 {
		t.Fatalf("header %+v, %d answers", h, n)
	}
	if st := tn.srv.Stats(); st.DNSEncrypted != 1 || st.DNSPlain != 0 {
		t.Fatalf("stats %+v", st)
	}
}

// An answer bigger than the client can take over UDP is truncated, so the client retries over TCP; a client that
// announced a bigger size with EDNS0 gets all of it.
func TestBigAnswersAreTruncatedForUDP(t *testing.T) {
	// 30 uncompressed A records: about 850 bytes, more than 512 and less than 1232.
	tn := newTunnel(t, sourceDialer{}, gateway.Options{Resolver: fakeResolver{n: 30}})
	ans := udpExchange(t, tn, dnsQuery(t, 7, 0))
	h, n := readHeader(t, ans)
	if !h.Truncated || n != 0 || len(ans) > 512 {
		t.Fatalf("plain client: truncated=%v answers=%d size=%d", h.Truncated, n, len(ans))
	}
	ans = udpExchange(t, tn, dnsQuery(t, 8, 1232))
	h, n = readHeader(t, ans)
	if h.Truncated || n != 30 {
		t.Fatalf("EDNS client: truncated=%v answers=%d size=%d", h.Truncated, n, len(ans))
	}
}

func TestDNSOverTCP(t *testing.T) {
	tn := newTunnel(t, sourceDialer{}, gateway.Options{Resolver: fakeResolver{n: 60}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := tn.tnet.DialContextTCPAddrPort(ctx, dnsServer)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	q := dnsQuery(t, 9, 0)
	c.Write(append([]byte{byte(len(q) >> 8), byte(len(q))}, q...))
	var size [2]byte
	if _, err := io.ReadFull(c, size[:]); err != nil {
		t.Fatal(err)
	}
	ans := make([]byte, int(size[0])<<8|int(size[1]))
	if _, err := io.ReadFull(c, ans); err != nil {
		t.Fatal(err)
	}
	if h, n := readHeader(t, ans); h.ID != 9 || n != 60 || h.Truncated {
		t.Fatalf("header %+v, %d answers", h, n)
	}
}

// A device with a DNS server of its own (8.8.8.8 in a TV, the router's address left from home) is answered by the
// gateway as well, over UDP and TCP; nothing goes out in plain.
func TestDNSToAnyServerIsAnswered(t *testing.T) {
	tn := newTunnel(t, sourceDialer{}, gateway.Options{Resolver: fakeResolver{n: 3}})
	for _, server := range []string{"8.8.8.8:53", "192.168.1.1:53", "[2001:4860:4860::8888]:53"} {
		h, n := readHeader(t, udpExchangeWith(t, tn, netip.MustParseAddrPort(server), dnsQuery(t, 21, 0)))
		if h.ID != 21 || !h.Response || n != 3 {
			t.Fatalf("%s: header %+v, %d answers", server, h, n)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := tn.tnet.DialContextTCPAddrPort(ctx, netip.MustParseAddrPort("9.9.9.9:53"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	q := dnsQuery(t, 22, 0)
	c.Write(append([]byte{byte(len(q) >> 8), byte(len(q))}, q...))
	var size [2]byte
	if _, err := io.ReadFull(c, size[:]); err != nil {
		t.Fatal(err)
	}
	ans := make([]byte, int(size[0])<<8|int(size[1]))
	if _, err := io.ReadFull(c, ans); err != nil {
		t.Fatal(err)
	}
	if h, n := readHeader(t, ans); h.ID != 22 || n != 3 {
		t.Fatalf("tcp: header %+v, %d answers", h, n)
	}
	if st := tn.srv.Stats(); st.DNSEncrypted != 4 {
		t.Fatalf("stats %+v", st)
	}
}

// Without IPv6 on the uplink, names have no IPv6 addresses as far as the clients can tell; IPv4 ones still do.
func TestAAAAWithheldWithoutIPv6(t *testing.T) {
	tn := newTunnel(t, sourceDialer{}, gateway.Options{Resolver: fakeResolver{n: 2}, IPv6: func() bool { return false }})
	h, n := readHeader(t, udpExchange(t, tn, dnsQueryOf(t, 31, 0, dnsmessage.TypeAAAA)))
	if h.ID != 31 || !h.Response || h.RCode != dnsmessage.RCodeSuccess || n != 0 {
		t.Fatalf("AAAA: header %+v, %d answers", h, n)
	}
	if h, n := readHeader(t, udpExchange(t, tn, dnsQuery(t, 32, 0))); h.ID != 32 || n != 2 {
		t.Fatalf("A: header %+v, %d answers", h, n)
	}
	// With IPv6, AAAA questions go to the Resolver like any other.
	tn6 := newTunnel(t, sourceDialer{}, gateway.Options{Resolver: fakeResolver{n: 2}, IPv6: func() bool { return true }})
	if _, n := readHeader(t, udpExchange(t, tn6, dnsQueryOf(t, 33, 0, dnsmessage.TypeAAAA))); n != 2 {
		t.Fatalf("AAAA with IPv6: %d answers", n)
	}
}

// When nothing can answer, the client hears so at once (SERVFAIL) instead of waiting for its own timeout.
func TestResolverFailureIsServfail(t *testing.T) {
	tn := newTunnel(t, sourceDialer{}, gateway.Options{Resolver: fakeResolver{fail: true}})
	h, _ := readHeader(t, udpExchange(t, tn, dnsQuery(t, 11, 0)))
	if h.ID != 11 || h.RCode != dnsmessage.RCodeServerFailure {
		t.Fatalf("header %+v", h)
	}
	if st := tn.srv.Stats(); st.DNSFailed != 1 {
		t.Fatalf("stats %+v", st)
	}
}
