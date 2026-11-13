package pairing_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"hotspot/core/internal/pairing"
	"hotspot/core/internal/provision"
	"hotspot/core/internal/wgkey"
)

type registrar struct {
	mu sync.Mutex
	st *provision.State
}

func (r *registrar) ServerPublicKey() wgkey.Key { return r.st.ServerPublicKey() }

func (r *registrar) Enroll(name string, public wgkey.Key) (pairing.Grant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, err := r.st.EnrollPeer(name, public)
	if err != nil {
		return pairing.Grant{}, err
	}
	return pairing.Grant{IPv4: p.IPv4, IPv6: p.IPv6, DNS: []netip.Addr{provision.GatewayV4, provision.GatewayV6}, MTU: provision.TunnelMTU, Port: 51820}, nil
}

type listener struct {
	approve bool
	called  chan struct{}
	name    string
	sas     string
}

func (l *listener) Approve(name, sas string) bool {
	l.name, l.sas = name, sas
	close(l.called)
	return l.approve
}

func open(t *testing.T, approve bool, window time.Duration) (*pairing.Server, *listener, *registrar) {
	t.Helper()
	reg := &registrar{st: provision.NewState()}
	ln := &listener{approve: approve, called: make(chan struct{})}
	srv, err := pairing.Listen("127.0.0.1:0", reg, ln, window)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv, ln, reg
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestPairApproved(t *testing.T) {
	srv, ln, reg := open(t, true, time.Minute)

	var shown string
	res, err := pairing.Pair(ctx(t), srv.Addr(), "DESKTOP-TEST", func(sas string) { shown = sas })
	if err != nil {
		t.Fatal(err)
	}
	if shown == "" || shown != ln.sas {
		t.Fatalf("numbers differ: computer %q, phone %q", shown, ln.sas)
	}
	if ln.name != "DESKTOP-TEST" {
		t.Fatalf("phone saw name %q", ln.name)
	}
	if res.Grant.IPv4 != netip.MustParseAddr("10.8.0.2") || res.Grant.Port != 51820 || len(res.Grant.DNS) != 2 {
		t.Fatalf("grant = %+v", res.Grant)
	}
	if res.Server != reg.st.ServerPublicKey() {
		t.Fatal("server key mismatch")
	}
	if len(reg.st.Peers) != 1 || reg.st.Peers[0].PublicKey != res.Private.Public() {
		t.Fatal("the phone did not register the computer's public key")
	}
	if !reg.st.Peers[0].PresharedKey.IsZero() {
		t.Fatal("a pre-shared key must not exist: it would have to cross the network")
	}
	select {
	case <-srv.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("window stayed open after a pairing")
	}
}

func TestPairDenied(t *testing.T) {
	srv, _, reg := open(t, false, time.Minute)
	_, err := pairing.Pair(ctx(t), srv.Addr(), "intruder", func(string) {})
	if !errors.Is(err, pairing.ErrDenied) {
		t.Fatalf("err = %v, want ErrDenied", err)
	}
	if len(reg.st.Peers) != 0 {
		t.Fatal("a refused device was registered")
	}
}

func TestRepairReplacesSameName(t *testing.T) {
	srv, _, reg := open(t, true, time.Minute)
	if _, err := reg.Enroll("DESKTOP-TEST", wgkey.Generate().Public()); err != nil {
		t.Fatal(err)
	}
	res, err := pairing.Pair(ctx(t), srv.Addr(), "DESKTOP-TEST", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.st.Peers) != 1 || reg.st.Peers[0].PublicKey != res.Private.Public() {
		t.Fatalf("peers = %+v", reg.st.Peers)
	}
}

func post(t *testing.T, url string, body any) (int, string) {
	t.Helper()
	data, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	s, _ := out["session"].(string)
	return resp.StatusCode, s
}

// An attacker who sees the commitment cannot reveal a different key: the
// phone must reject it before the user is even asked.
func TestRevealMustMatchCommitment(t *testing.T) {
	srv, ln, reg := open(t, true, time.Minute)
	base := "http://" + srv.Addr()

	honest := wgkey.Generate().Public()
	nonce := bytes.Repeat([]byte{7}, 16)
	h := sha256.New()
	h.Write([]byte("hotspot-pairing-commit-v1"))
	h.Write(honest[:])
	h.Write(nonce)

	status, session := post(t, base+"/v1/hello", map[string]any{"name": "pc", "commit": h.Sum(nil)})
	if status != 200 || session == "" {
		t.Fatalf("hello: %d", status)
	}
	other := wgkey.Generate().Public()
	status, _ = post(t, base+"/v1/reveal", map[string]any{"session": session, "public_key": other, "nonce": nonce})
	if status != http.StatusBadRequest {
		t.Fatalf("reveal with another key: status %d, want 400", status)
	}
	select {
	case <-ln.called:
		t.Fatal("the user was asked about a key that broke its commitment")
	default:
	}
	if len(reg.st.Peers) != 0 {
		t.Fatal("device registered")
	}
	// The session is burnt: even the honest key cannot reuse it.
	status, _ = post(t, base+"/v1/reveal", map[string]any{"session": session, "public_key": honest, "nonce": nonce})
	if status != http.StatusNotFound {
		t.Fatalf("reused session: status %d, want 404", status)
	}
}

func TestWindowExpires(t *testing.T) {
	srv, _, _ := open(t, true, 150*time.Millisecond)
	select {
	case <-srv.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("window did not close")
	}
	if _, err := pairing.Pair(ctx(t), srv.Addr(), "late", func(string) {}); err == nil {
		t.Fatal("paired through a closed window")
	}
}

func TestTooManyAttempts(t *testing.T) {
	srv, _, _ := open(t, true, time.Minute)
	base := "http://" + srv.Addr()
	commit := bytes.Repeat([]byte{1}, 32)
	var last int
	for i := 0; i < 8; i++ {
		last, _ = post(t, base+"/v1/hello", map[string]any{"name": "spam", "commit": commit})
		if last == http.StatusTooManyRequests {
			break
		}
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("no rate limit, last status %d", last)
	}
	select {
	case <-srv.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("window stayed open after the limit")
	}
}

func TestQRCodeRoundTrip(t *testing.T) {
	_, code, err := pairing.NewQR()
	if err != nil {
		t.Fatal(err)
	}
	text := code.String()
	if !strings.HasPrefix(text, pairing.QRPrefix) || len(text) > 80 {
		t.Fatalf("code text %q", text)
	}
	back, err := pairing.ParseQR(" " + text + "\n")
	if err != nil || back != code {
		t.Fatalf("parsed %+v, %v", back, err)
	}
	for _, bad := range []string{"", "HSPC1:", "HSPC1:abc", "hotspot://join?d=x", "WIFI:S:DIRECT-HS-x;P:abcdefgh;;", text[len(pairing.QRPrefix):]} {
		if _, err := pairing.ParseQR(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

// The computer waits until the phone has scanned its code; then it is admitted without anybody being asked, and
// it knows the answer came from that phone.
func TestPairQR(t *testing.T) {
	srv, ln, reg := open(t, false, time.Minute) // the user would refuse if asked: QR pairing must not ask
	priv, code, err := pairing.NewQR()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pairing.PairQR(ctx(t), srv.Addr(), "LAPTOP", priv, code); !errors.Is(err, pairing.ErrNotScanned) {
		t.Fatalf("before the scan: %v, want ErrNotScanned", err)
	}
	srv.Expect(&code)
	res, err := pairing.PairQR(ctx(t), srv.Addr(), "LAPTOP", priv, code)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ln.called:
		t.Fatal("the user was asked although the code was scanned")
	default:
	}
	if res.Server != reg.st.ServerPublicKey() || res.Grant.IPv4 != netip.MustParseAddr("10.8.0.2") || res.Grant.Port != 51820 {
		t.Fatalf("result %+v", res)
	}
	if len(reg.st.Peers) != 1 || reg.st.Peers[0].PublicKey != code.Public || reg.st.Peers[0].Name != "LAPTOP" {
		t.Fatalf("peers %+v", reg.st.Peers)
	}
	select {
	case <-srv.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("window stayed open after a pairing")
	}
}

// Someone who read the computer's public key off the network but not the secret off its screen gets nowhere.
func TestPairQRNeedsTheSecret(t *testing.T) {
	srv, _, reg := open(t, true, time.Minute)
	priv, code, _ := pairing.NewQR()
	srv.Expect(&code)
	forged := code
	forged.Secret[0] ^= 1
	if _, err := pairing.PairQR(ctx(t), srv.Addr(), "intruder", priv, forged); err == nil {
		t.Fatal("paired without the secret")
	}
	if len(reg.st.Peers) != 0 {
		t.Fatal("registered without the secret")
	}
	// The scanned computer itself still gets in.
	if _, err := pairing.PairQR(ctx(t), srv.Addr(), "LAPTOP", priv, code); err != nil {
		t.Fatal(err)
	}
}

// A stranger posing as the phone cannot prove that it scanned the code, so the computer refuses its answer.
func TestPairQRRefusesAnImpostorPhone(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"ipv4": "10.8.0.2", "port": 51820, "server_public_key": wgkey.Generate().Public(), "proof": bytes.Repeat([]byte{1}, 32),
		})
	}))
	defer fake.Close()
	priv, code, _ := pairing.NewQR()
	if _, err := pairing.PairQR(ctx(t), strings.TrimPrefix(fake.URL, "http://"), "LAPTOP", priv, code); err == nil {
		t.Fatal("took an answer without the phone's proof")
	}
}

// A phone with an older app has no QR pairing: the computer learns that and can compare numbers instead.
func TestPairQRWithAnOlderPhone(t *testing.T) {
	old := httptest.NewServer(http.NotFoundHandler())
	defer old.Close()
	priv, code, _ := pairing.NewQR()
	if _, err := pairing.PairQR(ctx(t), strings.TrimPrefix(old.URL, "http://"), "LAPTOP", priv, code); !errors.Is(err, pairing.ErrNoQR) {
		t.Fatalf("err = %v, want ErrNoQR", err)
	}
}

func TestSASDependsOnEveryInput(t *testing.T) {
	a, b := wgkey.Generate().Public(), wgkey.Generate().Public()
	n := bytes.Repeat([]byte{1}, 16)
	base := pairing.SAS(a, b, n)
	if len(base) != 6 {
		t.Fatalf("SAS %q is not six digits", base)
	}
	if pairing.SAS(a, b, n) != base {
		t.Fatal("SAS is not deterministic")
	}
	changed := 0
	for i := 0; i < 20; i++ {
		if pairing.SAS(wgkey.Generate().Public(), b, n) != base {
			changed++
		}
	}
	if changed < 18 {
		t.Fatal("SAS ignores the client key")
	}
}
