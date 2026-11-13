// Package pairing lets a new device join the gateway without any file or QR
// code transfer.
//
// The device generates its own WireGuard key pair and sends only the public
// key, so no secret ever crosses the network. To rule out a man in the middle,
// both screens show a short authentication string (SAS) derived from the two
// public keys, and the user approves on the phone only if the numbers match.
// The device commits to its key (hash) before it sees the phone's, so an
// attacker cannot search for a key pair that makes the two numbers collide.
//
// Protocol (HTTP/JSON, plain: nothing in it is secret):
//
//	POST /v1/hello  {name, commit}                  -> {session, server_public_key}
//	POST /v1/reveal {session, public_key, nonce}    -> {ipv4, ipv6, dns, mtu, port}
//
// reveal blocks until the user has decided on the phone.
//
// With a QR code no number needs comparing. The computer shows its public key and a random secret as a QR code
// (QRCode) and the phone scans it. The secret never crosses the network: each side proves with it (HMAC) that it
// is the one on the other end of the camera, so a stranger on the Wi-Fi network can neither join in the
// computer's place nor pose as the phone.
//
//	POST /v1/qr {name, public_key, nonce, proof}    -> {ipv4, ipv6, dns, mtu, port, server_public_key, proof}
//
// Until the phone has scanned that computer's code it answers 409, and the computer asks again a moment later.
package pairing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
	"unicode"

	"hotspot/core/internal/wgkey"
)

const (
	// Port is the TCP port the phone listens on while a pairing window is open.
	Port = 51821
	// DefaultWindow is how long a pairing window stays open.
	DefaultWindow = 3 * time.Minute

	maxAttempts = 5
	maxBody     = 4 << 10
	maxName     = 64
	nonceSize   = 16
)

// ErrDenied is returned to the device when the user refused on the phone.
var ErrDenied = errors.New("pairing: the pairing was refused on the phone")

// ErrNotScanned is returned to the computer while the phone has not scanned its QR code yet.
var ErrNotScanned = errors.New("pairing: the phone has not scanned the code yet")

// ErrNoQR is returned to the computer when the phone's app is too old to pair with a QR code.
var ErrNoQR = errors.New("pairing: the phone cannot pair with a QR code")

// QRPrefix starts the text of a computer's pairing QR code, so that the phone tells it from other codes.
const QRPrefix = "HSPC1:"

const qrSecretSize = 16

// QRCode is what a computer shows to be paired by camera: its WireGuard public key and a one-time secret.
type QRCode struct {
	Public wgkey.Key
	Secret [qrSecretSize]byte
}

// String is the text of the QR code.
func (q QRCode) String() string {
	return QRPrefix + base64.RawURLEncoding.EncodeToString(append(q.Public[:], q.Secret[:]...))
}

// ParseQR reads the text of a computer's QR code.
func ParseQR(s string) (QRCode, error) {
	var q QRCode
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(strings.TrimSpace(s), QRPrefix))
	if !strings.HasPrefix(strings.TrimSpace(s), QRPrefix) || err != nil || len(raw) != len(q.Public)+qrSecretSize {
		return QRCode{}, errors.New("pairing: not a computer's pairing code")
	}
	copy(q.Public[:], raw)
	copy(q.Secret[:], raw[len(q.Public):])
	if q.Public.IsZero() {
		return QRCode{}, errors.New("pairing: not a computer's pairing code")
	}
	return q, nil
}

// clientProof shows that the computer knows the secret of the code the phone scanned.
func clientProof(secret []byte, client wgkey.Key, nonce []byte) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("hotspot-qr-client-v1"))
	m.Write(client[:])
	m.Write(nonce)
	return m.Sum(nil)
}

// serverProof shows that the phone scanned the code, and binds its own key to the answer.
func serverProof(secret []byte, client, server wgkey.Key, nonce []byte) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("hotspot-qr-server-v1"))
	m.Write(client[:])
	m.Write(server[:])
	m.Write(nonce)
	return m.Sum(nil)
}

// Grant is what a paired device needs to build its tunnel configuration.
type Grant struct {
	IPv4 netip.Addr   `json:"ipv4"`
	IPv6 netip.Addr   `json:"ipv6"`
	DNS  []netip.Addr `json:"dns"`
	MTU  int          `json:"mtu"`
	Port int          `json:"port"` // WireGuard UDP port of the gateway
}

// Registrar is the gateway side of a pairing.
type Registrar interface {
	ServerPublicKey() wgkey.Key
	// Enroll registers the device and returns what it needs for its tunnel.
	Enroll(name string, public wgkey.Key) (Grant, error)
}

// Listener asks the user.
type Listener interface {
	// Approve is called once the device revealed its key. sas is the number the
	// device shows too. It blocks until the user decided.
	Approve(name, sas string) bool
}

// SAS returns the six digits both sides display.
func SAS(client, server wgkey.Key, nonce []byte) string {
	h := sha256.New()
	h.Write([]byte("hotspot-pairing-sas-v1"))
	h.Write(client[:])
	h.Write(server[:])
	h.Write(nonce)
	return fmt.Sprintf("%06d", binary.BigEndian.Uint32(h.Sum(nil)[:4])%1000000)
}

func commitment(client wgkey.Key, nonce []byte) []byte {
	h := sha256.New()
	h.Write([]byte("hotspot-pairing-commit-v1"))
	h.Write(client[:])
	h.Write(nonce)
	return h.Sum(nil)
}

func cleanName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		}
		if b.Len() >= maxName {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

// ---- server (the phone) ----

type outcome struct {
	grant  Grant
	denied bool
	err    error
}

type session struct {
	name     string
	commit   []byte
	revealed bool
	result   chan outcome
}

// Server is an open pairing window.
type Server struct {
	reg Registrar
	ln  Listener
	l   net.Listener
	srv *http.Server

	mu       sync.Mutex
	attempts int
	sessions map[string]*session
	expected *QRCode // the computer whose code the phone scanned; nil when none

	done  chan struct{}
	once  sync.Once
	timer *time.Timer
}

// Listen opens a pairing window on addr for the given duration. It closes
// itself after one pairing (accepted or refused), after too many attempts, or
// when the window ends.
func Listen(addr string, reg Registrar, ln Listener, window time.Duration) (*Server, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{reg: reg, ln: ln, l: l, sessions: make(map[string]*session), done: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/hello", s.hello)
	mux.HandleFunc("/v1/reveal", s.reveal)
	mux.HandleFunc("/v1/qr", s.qr)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	s.timer = time.AfterFunc(window, s.Close)
	go s.srv.Serve(l)
	return s, nil
}

// Expect admits, without asking, the computer whose QR code the phone has just scanned (nil: none). It is used
// once.
func (s *Server) Expect(code *QRCode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if code == nil {
		s.expected = nil
		return
	}
	c := *code
	s.expected = &c
}

// Addr is the address the window listens on.
func (s *Server) Addr() string { return s.l.Addr().String() }

// Done is closed when the window has ended.
func (s *Server) Done() <-chan struct{} { return s.done }

// Close ends the window.
func (s *Server) Close() {
	s.once.Do(func() {
		close(s.done)
		s.timer.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		s.srv.Shutdown(ctx)
	})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) hello(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Commit []byte `json:"commit"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := cleanName(req.Name)
	if name == "" || len(req.Commit) != sha256.Size {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	if s.attempts >= maxAttempts {
		s.mu.Unlock()
		http.Error(w, "too many attempts", http.StatusTooManyRequests)
		go s.Close()
		return
	}
	s.attempts++
	raw := make([]byte, 16)
	rand.Read(raw)
	id := hex.EncodeToString(raw)
	s.sessions[id] = &session{name: name, commit: req.Commit, result: make(chan outcome, 1)}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"session": id, "server_public_key": s.reg.ServerPublicKey()})
}

func (s *Server) reveal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Session   string    `json:"session"`
		PublicKey wgkey.Key `json:"public_key"`
		Nonce     []byte    `json:"nonce"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	sess := s.sessions[req.Session]
	if sess == nil || sess.revealed {
		s.mu.Unlock()
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}
	sess.revealed = true
	s.mu.Unlock()

	if len(req.Nonce) < nonceSize || req.PublicKey.IsZero() || !bytes.Equal(commitment(req.PublicKey, req.Nonce), sess.commit) {
		http.Error(w, "key does not match the commitment", http.StatusBadRequest)
		return
	}

	sas := SAS(req.PublicKey, s.reg.ServerPublicKey(), req.Nonce)
	go func() {
		if !s.ln.Approve(sess.name, sas) {
			sess.result <- outcome{denied: true}
			return
		}
		g, err := s.reg.Enroll(sess.name, req.PublicKey)
		sess.result <- outcome{grant: g, err: err}
	}()

	select {
	case out := <-sess.result:
		switch {
		case out.denied:
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "denied"})
		case out.err != nil:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": out.err.Error()})
		default:
			writeJSON(w, http.StatusOK, out.grant)
		}
		go s.Close() // one pairing per window
	case <-s.done:
		http.Error(w, "pairing window closed", http.StatusServiceUnavailable)
	case <-r.Context().Done():
	}
}

// qrAnswer is the phone's answer to a computer whose code it scanned.
type qrAnswer struct {
	Grant
	ServerPublicKey wgkey.Key `json:"server_public_key"`
	Proof           []byte    `json:"proof"`
}

func (s *Server) qr(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string    `json:"name"`
		PublicKey wgkey.Key `json:"public_key"`
		Nonce     []byte    `json:"nonce"`
		Proof     []byte    `json:"proof"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := cleanName(req.Name)
	if name == "" || req.PublicKey.IsZero() || len(req.Nonce) < nonceSize || len(req.Proof) != sha256.Size {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	exp := s.expected
	if exp == nil || exp.Public != req.PublicKey {
		// Not scanned (yet): the computer asks again in a moment. This costs no attempt, since only the scanned
		// computer's key gets past here, and it still needs the secret.
		s.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "not scanned"})
		return
	}
	if !hmac.Equal(req.Proof, clientProof(exp.Secret[:], req.PublicKey, req.Nonce)) {
		s.attempts++
		tooMany := s.attempts >= maxAttempts
		s.mu.Unlock()
		http.Error(w, "wrong proof", http.StatusUnauthorized)
		if tooMany {
			go s.Close()
		}
		return
	}
	s.expected = nil // one pairing per scan
	s.mu.Unlock()

	g, err := s.reg.Enroll(name, req.PublicKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	server := s.reg.ServerPublicKey()
	writeJSON(w, http.StatusOK, qrAnswer{Grant: g, ServerPublicKey: server, Proof: serverProof(exp.Secret[:], req.PublicKey, server, req.Nonce)})
	go s.Close() // one pairing per window
}

// ---- client (the computer) ----

// NewQR makes this computer's key pair and the QR code that lets the phone admit it.
func NewQR() (wgkey.Key, QRCode, error) {
	priv := wgkey.Generate()
	q := QRCode{Public: priv.Public()}
	if _, err := rand.Read(q.Secret[:]); err != nil {
		return wgkey.Key{}, QRCode{}, err
	}
	return priv, q, nil
}

// PairQR asks the phone at phone ("host:port") to admit this computer, whose code (made with NewQR from priv) the
// phone scans. It returns ErrNotScanned while the phone has not scanned it, and ErrNoQR if the phone's app does
// not know QR pairing. The phone's answer is taken only with its proof of having scanned the code.
func PairQR(ctx context.Context, phone, name string, priv wgkey.Key, code QRCode) (*Result, error) {
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	pub := priv.Public()
	var ans qrAnswer
	err := post(ctx, "http://"+phone+"/v1/qr", map[string]any{
		"name": name, "public_key": pub, "nonce": nonce, "proof": clientProof(code.Secret[:], pub, nonce),
	}, &ans)
	var se *statusError
	if errors.As(err, &se) {
		switch se.code {
		case http.StatusConflict:
			return nil, ErrNotScanned
		case http.StatusNotFound:
			return nil, ErrNoQR
		}
	}
	if err != nil {
		return nil, err
	}
	if ans.ServerPublicKey.IsZero() || !hmac.Equal(ans.Proof, serverProof(code.Secret[:], pub, ans.ServerPublicKey, nonce)) {
		return nil, errors.New("pairing: the answer did not come from the phone that scanned the code")
	}
	if !ans.IPv4.IsValid() || ans.Port == 0 {
		return nil, errors.New("pairing: the phone sent an incomplete configuration")
	}
	return &Result{Private: priv, Server: ans.ServerPublicKey, Grant: ans.Grant}, nil
}

// Result is a completed pairing.
type Result struct {
	Private wgkey.Key // this device's private key; never leaves it
	Server  wgkey.Key // the gateway's public key
	Grant   Grant
}

// Pair asks the phone at phone ("host:port") to admit this device. showSAS is
// called with the number that must match the one on the phone's screen; Pair
// then blocks until the user decided there.
func Pair(ctx context.Context, phone, name string, showSAS func(sas string)) (*Result, error) {
	priv := wgkey.Generate()
	pub := priv.Public()
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	base := "http://" + phone

	var hello struct {
		Session         string    `json:"session"`
		ServerPublicKey wgkey.Key `json:"server_public_key"`
	}
	if err := post(ctx, base+"/v1/hello", map[string]any{"name": name, "commit": commitment(pub, nonce)}, &hello); err != nil {
		return nil, err
	}
	if hello.ServerPublicKey.IsZero() || hello.Session == "" {
		return nil, errors.New("pairing: the phone sent an incomplete answer")
	}
	showSAS(SAS(pub, hello.ServerPublicKey, nonce))

	var grant Grant
	if err := post(ctx, base+"/v1/reveal", map[string]any{"session": hello.Session, "public_key": pub, "nonce": nonce}, &grant); err != nil {
		return nil, err
	}
	if !grant.IPv4.IsValid() || grant.Port == 0 {
		return nil, errors.New("pairing: the phone sent an incomplete configuration")
	}
	return &Result{Private: priv, Server: hello.ServerPublicKey, Grant: grant}, nil
}

func post(ctx context.Context, url string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		if resp.StatusCode == http.StatusForbidden {
			return ErrDenied
		}
		return &statusError{code: resp.StatusCode, msg: fmt.Sprintf("pairing: the phone answered %s: %s", resp.Status, strings.TrimSpace(string(msg)))}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(out)
}

// statusError is an answer of the phone other than OK.
type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }
