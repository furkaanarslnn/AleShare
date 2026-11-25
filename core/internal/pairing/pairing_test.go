package pairing_test

import (
	"bytes"
	"context"
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

func open(t *testing.T, window time.Duration) (*pairing.Server, *registrar) {
	t.Helper()
	reg := &registrar{st: provision.NewState()}
	srv, err := pairing.Listen("127.0.0.1:0", reg, window)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv, reg
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

func post(t *testing.T, url string, body any) int {
	t.Helper()
	data, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// The number pairing is gone: someone between the computer and the phone could make the two numbers match.
func TestNumberPairingIsGone(t *testing.T) {
	srv, reg := open(t, time.Minute)
	base := "http://" + srv.Addr()
	if code := post(t, base+"/v1/hello", map[string]any{"name": "old", "commit": bytes.Repeat([]byte{1}, 32)}); code != http.StatusGone {
		t.Fatalf("hello: %d", code)
	}
	if code := post(t, base+"/v1/reveal", map[string]any{"session": "x"}); code != http.StatusGone {
		t.Fatalf("reveal: %d", code)
	}
	if len(reg.st.Peers) != 0 {
		t.Fatal("enrolled")
	}
}

func TestRepairReplacesSameName(t *testing.T) {
	srv, reg := open(t, time.Minute)
	if _, err := reg.Enroll("DESKTOP-TEST", wgkey.Generate().Public()); err != nil {
		t.Fatal(err)
	}
	priv, code, _ := pairing.NewQR()
	srv.Expect(&code)
	res, err := pairing.PairQR(ctx(t), srv.Addr(), "DESKTOP-TEST", priv, code)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.st.Peers) != 1 || reg.st.Peers[0].PublicKey != res.Private.Public() {
		t.Fatalf("peers = %+v", reg.st.Peers)
	}
}

func TestWindowExpires(t *testing.T) {
	srv, _ := open(t, 150*time.Millisecond)
	select {
	case <-srv.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("window did not close")
	}
	priv, code, _ := pairing.NewQR()
	srv.Expect(&code)
	if _, err := pairing.PairQR(ctx(t), srv.Addr(), "late", priv, code); err == nil {
		t.Fatal("paired through a closed window")
	}
}

// Wrong proofs for the scanned computer's key close the window after a few.
func TestTooManyAttempts(t *testing.T) {
	srv, _ := open(t, time.Minute)
	priv, code, _ := pairing.NewQR()
	srv.Expect(&code)
	forged := code
	forged.Secret[0] ^= 1
	for i := 0; i < 8; i++ {
		pairing.PairQR(ctx(t), srv.Addr(), "spam", priv, forged)
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
	if !strings.HasPrefix(text, pairing.QRPrefix2) || len(text) > 80 {
		t.Fatalf("code text %q", text)
	}
	back, err := pairing.ParseQR(" " + text + "\n")
	if err != nil || back != code {
		t.Fatalf("parsed %+v, %v", back, err)
	}
	// A code of the first form (computers up to 1.2.5) is still read.
	old := code
	old.Named = false
	if back, err := pairing.ParseQR(old.String()); err != nil || back != old || !strings.HasPrefix(old.String(), pairing.QRPrefix) {
		t.Fatalf("first form: %+v, %v", back, err)
	}
	for _, bad := range []string{"", "HSPC1:", "HSPC2:abc", "hotspot://join?d=x", "WIFI:S:DIRECT-HS-x;P:abcdefgh;;", text[len(pairing.QRPrefix2):]} {
		if _, err := pairing.ParseQR(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

// The computer waits until the phone has scanned its code; then it is admitted without anybody being asked, and
// it knows the answer came from that phone.
func TestPairQR(t *testing.T) {
	srv, reg := open(t, time.Minute)
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
	srv, reg := open(t, time.Minute)
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

// A phone with an app older than QR pairing: the computer learns that and tells its user to update the phone.
func TestPairQRWithAnOlderPhone(t *testing.T) {
	old := httptest.NewServer(http.NotFoundHandler())
	defer old.Close()
	priv, code, _ := pairing.NewQR()
	if _, err := pairing.PairQR(ctx(t), strings.TrimPrefix(old.URL, "http://"), "LAPTOP", priv, code); !errors.Is(err, pairing.ErrNoQR) {
		t.Fatalf("err = %v, want ErrNoQR", err)
	}
}

// The name is in the proof of a code of the second form: someone on the way who renames the computer gets it
// refused. A computer of the first form (up to 1.2.5) still pairs, without that protection.
func TestPairQRNameIsProved(t *testing.T) {
	srv, reg := open(t, time.Minute)
	priv, code, _ := pairing.NewQR()
	srv.Expect(&code)
	nonce := bytes.Repeat([]byte{3}, 16)
	// The computer's request as the computer makes it, its name then changed by someone on the way.
	proof := pairing.ProofForTest(code, priv.Public(), nonce, "LAPTOP")
	if c := post(t, "http://"+srv.Addr()+"/v1/qr", map[string]any{"name": "EVIL", "public_key": priv.Public(), "nonce": nonce, "proof": proof}); c != http.StatusUnauthorized {
		t.Fatalf("renamed on the way: %d", c)
	}
	if len(reg.st.Peers) != 0 {
		t.Fatal("enrolled under another name")
	}
	if _, err := pairing.PairQR(ctx(t), srv.Addr(), "LAPTOP", priv, code); err != nil || reg.st.Peers[0].Name != "LAPTOP" {
		t.Fatalf("the computer itself: %v, %+v", err, reg.st.Peers)
	}

	srv2, reg2 := open(t, time.Minute)
	priv2, old, _ := pairing.NewQR()
	old.Named = false
	srv2.Expect(&old)
	if _, err := pairing.PairQR(ctx(t), srv2.Addr(), "OLD-PC", priv2, old); err != nil || reg2.st.Peers[0].Name != "OLD-PC" {
		t.Fatalf("a computer of the first form: %v", err)
	}
}
