// Package pairing lets a computer join the gateway without any file or secret crossing the network.
//
// The computer makes its own WireGuard key pair and shows its public key and a random one-time secret as a QR
// code (QRCode); the phone's camera reads it. The secret never crosses the network: each side proves with it
// (HMAC) that it is the one on the other end of the camera, so a stranger on the Wi-Fi network can neither join in
// the computer's place nor pose as the phone. A code of the second form ("HSPC2:") also binds the computer's name
// into the proof, so that nobody on the way can rename the computer it admits.
//
//	POST /v1/qr {name, public_key, nonce, proof}    -> {ipv4, ipv6, dns, mtu, port, server_public_key, proof}
//
// Until the phone has scanned that computer's code it answers 409, and the computer asks again a moment later.
//
// Up to 1.2.5 a computer could also pair by comparing a six-digit number (/v1/hello, /v1/reveal). Someone on the
// network between the two could make both screens show the same number (they choose their own key after seeing
// the computer's commitment, then try keys until the number matches), so that way is gone: the phone answers it
// with 410 and the computer only pairs by QR code.
package pairing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
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

// ErrNotScanned is returned to the computer while the phone has not scanned its QR code yet.
var ErrNotScanned = errors.New("pairing: the phone has not scanned the code yet")

// ErrNoQR is returned to the computer when the phone's app is too old to pair with a QR code.
var ErrNoQR = errors.New("pairing: the phone cannot pair with a QR code")

// QRPrefix starts the text of a computer's pairing QR code, so that the phone tells it from other codes; a code of
// the second form, which binds the computer's name, starts with QRPrefix2.
const (
	QRPrefix  = "HSPC1:"
	QRPrefix2 = "HSPC2:"
)

const qrSecretSize = 16

// QRCode is what a computer shows to be paired by camera: its WireGuard public key and a one-time secret.
type QRCode struct {
	Public wgkey.Key
	Secret [qrSecretSize]byte
	// Named: the computer proves its name too (the second form). The phone then takes no other name for it.
	Named bool
}

// String is the text of the QR code.
func (q QRCode) String() string {
	prefix := QRPrefix
	if q.Named {
		prefix = QRPrefix2
	}
	return prefix + base64.RawURLEncoding.EncodeToString(append(q.Public[:], q.Secret[:]...))
}

// ParseQR reads the text of a computer's QR code, of either form.
func ParseQR(s string) (QRCode, error) {
	var q QRCode
	s = strings.TrimSpace(s)
	rest, ok := strings.CutPrefix(s, QRPrefix2)
	if ok {
		q.Named = true
	} else if rest, ok = strings.CutPrefix(s, QRPrefix); !ok {
		return QRCode{}, errors.New("pairing: not a computer's pairing code")
	}
	raw, err := base64.RawURLEncoding.DecodeString(rest)
	if err != nil || len(raw) != len(q.Public)+qrSecretSize {
		return QRCode{}, errors.New("pairing: not a computer's pairing code")
	}
	copy(q.Public[:], raw)
	copy(q.Secret[:], raw[len(q.Public):])
	if q.Public.IsZero() {
		return QRCode{}, errors.New("pairing: not a computer's pairing code")
	}
	return q, nil
}

// clientProof shows that the computer knows the secret of the code the phone scanned. For a code of the second
// form (named) it covers the name the computer gives itself as well.
func clientProof(secret []byte, client wgkey.Key, nonce []byte, named bool, name string) []byte {
	m := hmac.New(sha256.New, secret)
	if named {
		m.Write([]byte("hotspot-qr-client-v2"))
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(name)))
		m.Write(n[:])
		m.Write([]byte(name))
	} else {
		m.Write([]byte("hotspot-qr-client-v1"))
	}
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

// Server is an open pairing window.
type Server struct {
	reg Registrar
	l   net.Listener
	srv *http.Server

	mu       sync.Mutex
	attempts int
	expected *QRCode // the computer whose code the phone scanned; nil when none

	done  chan struct{}
	once  sync.Once
	timer *time.Timer
}

// Listen opens a pairing window on addr for the given duration. It closes
// itself after one pairing, after too many wrong proofs, or when the window ends.
func Listen(addr string, reg Registrar, window time.Duration) (*Server, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{reg: reg, l: l, done: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/hello", gone)
	mux.HandleFunc("/v1/reveal", gone)
	mux.HandleFunc("/v1/qr", s.qr)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
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

// gone answers the number pairing of computers older than 1.0.10: it is no longer offered (see the package
// comment). Such a computer says that the phone refused, and its user updates it.
func gone(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "number pairing is no longer offered: update AleShare on the computer and pair with the QR code", http.StatusGone)
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
	if !hmac.Equal(req.Proof, clientProof(exp.Secret[:], req.PublicKey, req.Nonce, exp.Named, req.Name)) {
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

// NewQR makes this computer's key pair and the QR code that lets the phone admit it (of the second form, which
// binds the name; a phone older than 1.2.6 does not read it, and is updated first).
func NewQR() (wgkey.Key, QRCode, error) {
	priv := wgkey.Generate()
	q := QRCode{Public: priv.Public(), Named: true}
	if _, err := rand.Read(q.Secret[:]); err != nil {
		return wgkey.Key{}, QRCode{}, err
	}
	return priv, q, nil
}

// PairQR asks the phone at phone ("host:port") to admit this computer, whose code (made with NewQR from priv) the
// phone scans. It returns ErrNotScanned while the phone has not scanned it, and ErrNoQR if the phone's app is
// older than QR pairing. The phone's answer is taken only with its proof of having scanned the code.
func PairQR(ctx context.Context, phone, name string, priv wgkey.Key, code QRCode) (*Result, error) {
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	pub := priv.Public()
	var ans qrAnswer
	err := post(ctx, "http://"+phone+"/v1/qr", map[string]any{
		"name": name, "public_key": pub, "nonce": nonce, "proof": clientProof(code.Secret[:], pub, nonce, code.Named, name),
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
