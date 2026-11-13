package wgcore

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hotspot/core/internal/release"
)

// testStore is a phone's store with a test release key and an HTTP server in front of it.
type testStore struct {
	u    *updates
	srv  *httptest.Server
	dir  string
	key  ed25519.PrivateKey
	tok  string
	told int // how often what is on offer changed
}

func newTestStore(t *testing.T, accept func(netip.Addr) bool) *testStore {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	s := &testStore{u: newUpdates(), dir: t.TempDir(), key: priv}
	s.u.verify = func(b []byte) (release.Info, error) { return release.VerifyWith(b, pub) }
	s.u.changed = func() { s.told++ }
	s.u.store = s.dir
	_, _, s.tok = s.u.offers()
	s.srv = httptest.NewServer(s.u.handler(accept))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *testStore) do(t *testing.T, method, path string, body []byte) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, s.srv.URL+path, bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(got)
}

func (s *testStore) program(code int) []byte {
	return release.Sign([]byte("MZ program "+strings.Repeat("x", code)), release.Info{Code: code, Name: "1.0." + string(rune('0'+code%10))}, s.key)
}

func local(a netip.Addr) bool { return a.IsLoopback() }

// The phone hands out its files only to senders it accepts that know the token, and only at their paths.
func TestStoreHandsOut(t *testing.T) {
	s := newTestStore(t, local)
	apk := filepath.Join(s.dir, "base.apk")
	os.WriteFile(apk, []byte("PK an apk"), 0o600)
	s.u.setApp(releaseFile{apk, 11, "1.0.11"})

	if code, body := s.do(t, "GET", "/hotspot.apk?t="+s.tok, nil); code != 200 || body != "PK an apk" {
		t.Fatalf("good request: %d %q", code, body)
	}
	for name, path := range map[string]string{
		"wrong token":       "/hotspot.apk?t=guess",
		"no token":          "/hotspot.apk",
		"other file":        "/base.apk?t=" + s.tok,
		"no program yet":    "/hotspot-pc.exe?t=" + s.tok,
		"the folder itself": "/?t=" + s.tok,
	} {
		if code, _ := s.do(t, "GET", path, nil); code != http.StatusNotFound {
			t.Errorf("%s: %d", name, code)
		}
	}
	stranger := newTestStore(t, func(netip.Addr) bool { return false })
	stranger.u.setApp(releaseFile{apk, 11, "1.0.11"})
	if code, _ := stranger.do(t, "GET", "/hotspot.apk?t="+stranger.tok, nil); code != http.StatusNotFound {
		t.Errorf("sender the phone does not accept: %d", code)
	}
}

// A device brings a newer APK: the phone keeps it for the app to check, once; an older one is turned away.
func TestStoreTakesANewerAPK(t *testing.T) {
	s := newTestStore(t, local)
	s.u.setApp(releaseFile{"base.apk", 11, "1.0.11"})
	if code, _ := s.do(t, "POST", "/hotspot.apk?t="+s.tok+"&code=11", []byte("PK same")); code != http.StatusConflict {
		t.Fatalf("same release: %d", code)
	}
	if code, _ := s.do(t, "POST", "/hotspot.apk?t=guess&code=12", []byte("PK")); code != http.StatusNotFound {
		t.Fatalf("without the token: %d", code)
	}
	if code, body := s.do(t, "POST", "/hotspot.apk?t="+s.tok+"&code=12", []byte("PK newer")); code != 200 {
		t.Fatalf("newer: %d %s", code, body)
	}
	got := s.u.takeReceived()
	if got == nil || got.code != 12 {
		t.Fatalf("received %+v", got)
	}
	if data, _ := os.ReadFile(got.path); string(data) != "PK newer" {
		t.Fatalf("kept %q", data)
	}
	if s.u.takeReceived() != nil {
		t.Fatal("handed over twice")
	}
}

// A computer brings a newer Windows program: taken only with the release signature, then handed to others.
func TestStoreTakesASignedProgram(t *testing.T) {
	s := newTestStore(t, local)
	if code, _ := s.do(t, "POST", "/hotspot-pc.exe?t="+s.tok, []byte("MZ unsigned")); code != http.StatusForbidden {
		t.Fatalf("unsigned: %d", code)
	}
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	foreign := release.Sign([]byte("MZ"), release.Info{Code: 20, Name: "1.0.20"}, other)
	if code, _ := s.do(t, "POST", "/hotspot-pc.exe?t="+s.tok, foreign); code != http.StatusForbidden {
		t.Fatalf("signed by another key: %d", code)
	}
	v12 := s.program(12)
	if code, body := s.do(t, "POST", "/hotspot-pc.exe?t="+s.tok, v12); code != 200 {
		t.Fatalf("signed: %d %s", code, body)
	}
	if code, _ := s.do(t, "POST", "/hotspot-pc.exe?t="+s.tok, s.program(11)); code != http.StatusConflict {
		t.Fatalf("older: %d", code)
	}
	if _, pc, _ := s.u.offers(); pc.code != 12 || s.told == 0 {
		t.Fatalf("on offer %+v, told %d", pc, s.told)
	}
	if code, body := s.do(t, "GET", "/hotspot-pc.exe?t="+s.tok, nil); code != 200 || body != string(v12) {
		t.Fatalf("handing it out: %d (%d bytes)", code, len(body))
	}
	// A phone that starts again offers what it kept.
	again := newTestStore(t, local)
	again.u.verify = s.u.verify
	again.u.store = s.dir
	if _, err := again.u.setProgram(filepath.Join(s.dir, "hotspot-pc.exe")); err != nil {
		t.Fatal(err)
	}
	if _, pc, _ := again.u.offers(); pc.code != 12 {
		t.Fatalf("after a restart: %+v", pc)
	}
}

// A computer reads the phone's state and asks for changes; the app takes them, once. Only plain JSON objects go.
func TestPhoneStatusAndControls(t *testing.T) {
	s := newTestStore(t, local)
	if code, _ := s.do(t, "GET", "/phone?t="+s.tok, nil); code != http.StatusNotFound {
		t.Fatalf("state before the app told any: %d", code)
	}
	s.u.status = `{"battery":81}`
	if code, body := s.do(t, "GET", "/phone?t="+s.tok, nil); code != 200 || body != `{"battery":81}` {
		t.Fatalf("state: %d %q", code, body)
	}
	if code, _ := s.do(t, "GET", "/phone?t=guess", nil); code != http.StatusNotFound {
		t.Fatalf("state without the token: %d", code)
	}
	for name, body := range map[string]string{
		"not JSON": "split=on", "empty": "{}", "a list": `[true]`, "nested": `{"split":{"on":true}}`,
		"too big": `{"x":"` + strings.Repeat("a", maxControl) + `"}`,
	} {
		if code, _ := s.do(t, "POST", "/phone?t="+s.tok, []byte(body)); code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, code)
		}
	}
	s.do(t, "POST", "/phone?t="+s.tok, []byte(`{"split":true}`))
	s.do(t, "POST", "/phone?t="+s.tok, []byte(`{"limit_mb":2048}`))
	c := &Core{upd: s.u}
	if got := c.TakeControls(); got != "{\"split\":true}\n{\"limit_mb\":2048}" {
		t.Fatalf("controls %q", got)
	}
	if c.TakeControls() != "" {
		t.Fatal("handed over twice")
	}
}

// The store's listener inside the tunnel hands the server what the gateway takes, and nothing after it closed.
func TestTunnelListener(t *testing.T) {
	l := newTunnelListener()
	a, b := net.Pipe()
	go l.take(a)
	if c, err := l.Accept(); err != nil || c != a {
		t.Fatalf("accept: %v %v", c, err)
	}
	l.Close()
	if _, err := l.Accept(); err != net.ErrClosed {
		t.Fatalf("after close: %v", err)
	}
	l.take(b) // must not block; the connection is closed instead
	if _, err := b.Write([]byte("x")); err == nil {
		t.Fatal("a connection taken after close stayed open")
	}
}

func TestAcceptUpdate(t *testing.T) {
	for addr, want := range map[string]bool{
		"192.168.49.23": true, "10.8.0.2": true, "fd42:42:42::2": true,
		"192.168.1.10": false, "10.9.0.2": false, "8.8.8.8": false,
	} {
		if acceptUpdate(netip.MustParseAddr(addr)) != want {
			t.Errorf("%s: want %v", addr, want)
		}
	}
}

// The app's token survives its restarts; anything that is not one is ignored.
func TestSetUpdateToken(t *testing.T) {
	c, _ := NewCore("")
	_, _, before := c.upd.offers()
	c.SetUpdateToken("not a token")
	if _, _, got := c.upd.offers(); got != before {
		t.Fatal("took a token that is not 32 hex digits")
	}
	kept := "0123456789abcdef0123456789abcdef"
	c.SetUpdateToken(kept)
	if _, _, got := c.upd.offers(); got != kept {
		t.Fatalf("token %q, want the app's", got)
	}
}
