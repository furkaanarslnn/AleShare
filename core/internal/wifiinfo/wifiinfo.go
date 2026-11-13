// Package wifiinfo lets a paired device ask the phone for the name and password of the Wi-Fi network the
// phone opens, so that the device follows when the user changes them.
//
// Only a device that holds the private key of a paired peer can ask, and only it can read the answer: the
// phone and the device derive a secret from their WireGuard keys (the X25519 of one's private key and the
// other's public key, which WireGuard's own handshake uses too). Nothing in a request names the device, so a
// request sent to a network that turns out not to be the phone's tells that network nothing.
//
// A request is "HSW1", a random challenge and a tag over it; the answer is "HSW1", a nonce and the JSON of
// Info sealed with AES-GCM under a key bound to the challenge, so an old answer cannot be played back.
package wifiinfo

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"hotspot/core/internal/wgkey"
)

// Port is where the phone answers: on its Wi-Fi network, and on its address inside the tunnel.
const Port = 51822

// Network is a Wi-Fi network the phone opens.
type Network struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
}

// Valid reports whether n could be the phone's network: a Wi-Fi Direct name and a WPA2 passphrase.
func (n Network) Valid() bool {
	return strings.HasPrefix(n.SSID, "DIRECT-") && len(n.SSID) <= 32 && len(n.Password) >= 8 && len(n.Password) <= 63
}

// Info is what the phone tells: the network it opens now and, while a change is under way, the one that
// follows. A device that knows both joins whichever is there.
type Info struct {
	Current Network  `json:"current"`
	Next    *Network `json:"next,omitempty"`
	// App is the newest app (APK) the phone has, so that a device with an older one can take it from the phone,
	// and one with a newer one can bring it.
	App *App `json:"app,omitempty"`
	// PC is the same for the Windows program; Code 0 when the phone has none yet.
	PC *App `json:"pc,omitempty"`
}

// App says which release the phone has and how to download it from the phone, or bring it a newer one. Token is a
// secret of the running phone that only paired devices learn (in this sealed answer); both ways ask for it.
type App struct {
	Code  int    `json:"code"`
	Name  string `json:"name"`
	Token string `json:"token"`
	// DeviceToken is what the devices named with SetDevices are told as their token in place of Token: the phone
	// gives them fewer rights (see wgcore, update.go). It never travels under this name.
	DeviceToken string `json:"-"`
}

// forDevice is a as a device named with SetDevices is told it.
func (a *App) forDevice() *App {
	if a == nil {
		return nil
	}
	c := *a
	c.Token = c.DeviceToken // empty when the phone made none: then the device gets no token, never the other one
	return &c
}

const (
	magic        = "HSW1"
	challengeLen = 16
	tagLen       = 16
	requestLen   = len(magic) + challengeLen + tagLen
	nonceLen     = 12

	// A device asks again this soon when no answer came; UDP may lose a packet.
	retryAfter = 700 * time.Millisecond
	// The phone answers each paired device this many requests in a burst and this many a second after that. What
	// comes from nobody paired gets no answer and uses none of it up.
	burst   = 20
	perSec  = 10
	maxRead = 2048
)

// ErrNoAnswer means that nobody answered in time: not the phone's network, the phone's app is not running,
// or this device is no longer paired.
var ErrNoAnswer = errors.New("wifiinfo: no answer from the phone")

// pairKey is the secret that the phone and one device share.
func pairKey(private, public wgkey.Key) ([]byte, error) {
	priv, err := ecdh.X25519().NewPrivateKey(private[:])
	if err != nil {
		return nil, err
	}
	pub, err := ecdh.X25519().NewPublicKey(public[:])
	if err != nil {
		return nil, err
	}
	secret, err := priv.ECDH(pub) // fails on a low-order public key
	if err != nil {
		return nil, err
	}
	return mac(secret, []byte("hotspot wifi info v1")), nil
}

func mac(key []byte, parts ...[]byte) []byte {
	h := hmac.New(sha256.New, key)
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

func requestTag(k, challenge []byte) []byte { return mac(k, []byte("request"), challenge)[:tagLen] }

func answerAEAD(k, challenge []byte) cipher.AEAD {
	block, err := aes.NewCipher(mac(k, []byte("answer"), challenge))
	if err != nil {
		panic(err) // a 32-byte key always makes a cipher
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return aead
}

func additional(challenge []byte) []byte { return append([]byte(magic), challenge...) }

// Responder answers the requests on the phone.
type Responder struct {
	private wgkey.Key

	mu      sync.Mutex
	peers   map[wgkey.Key][]byte // public key of each paired device → pair key
	info    *Info                // nil: nothing to tell
	learned map[wgkey.Key]bool   // devices that were told the current Next
	asked   map[wgkey.Key]bool   // devices that asked at all: those follow a change by themselves
	devices map[wgkey.Key]bool   // devices that are told DeviceToken (SetDevices)
	rates   map[wgkey.Key]*rate  // how much each device may still ask
}

// rate is one device's allowance of requests.
type rate struct {
	left   float64
	filled time.Time
}

// NewResponder makes the phone's side. private is the WireGuard server key.
func NewResponder(private wgkey.Key) *Responder {
	return &Responder{private: private, peers: map[wgkey.Key][]byte{}, learned: map[wgkey.Key]bool{}, asked: map[wgkey.Key]bool{}, rates: map[wgkey.Key]*rate{}}
}

// SetDevices names the devices that are told an App's DeviceToken in place of its Token.
func (r *Responder) SetDevices(public []wgkey.Key) {
	devices := make(map[wgkey.Key]bool, len(public))
	for _, pub := range public {
		devices[pub] = true
	}
	r.mu.Lock()
	r.devices = devices
	r.mu.Unlock()
}

// Asked lists the devices that have asked since the responder was made. A device that never asks (one with the
// official WireGuard app) cannot follow a change, so there is no point waiting for it.
func (r *Responder) Asked() []wgkey.Key {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []wgkey.Key
	for k := range r.asked {
		if _, still := r.peers[k]; still {
			out = append(out, k)
		}
	}
	return out
}

// SetPeers replaces the devices that may ask.
func (r *Responder) SetPeers(public []wgkey.Key) {
	r.mu.Lock()
	defer r.mu.Unlock()
	peers := make(map[wgkey.Key][]byte, len(public))
	for _, pub := range public {
		if k, ok := r.peers[pub]; ok {
			peers[pub] = k
		} else if k, err := pairKey(r.private, pub); err == nil {
			peers[pub] = k
		}
	}
	r.peers = peers
	for pub := range r.rates {
		if _, still := peers[pub]; !still {
			delete(r.rates, pub)
		}
	}
}

// SetInfo sets what the devices are told; nil tells nothing. A different Next starts the count of Learned anew.
func (r *Responder) SetInfo(info *Info) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var oldNext, newNext *Network
	if r.info != nil {
		oldNext = r.info.Next
	}
	if info != nil {
		c := *info
		if c.Next != nil {
			n := *c.Next
			c.Next = &n
		}
		if c.App != nil {
			a := *c.App
			c.App = &a
		}
		if c.PC != nil {
			p := *c.PC
			c.PC = &p
		}
		info = &c
		newNext = info.Next
	}
	if !sameNetwork(oldNext, newNext) {
		r.learned = map[wgkey.Key]bool{}
	}
	r.info = info
}

func sameNetwork(a, b *Network) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Learned lists the devices that have been told the current Next.
func (r *Responder) Learned() []wgkey.Key {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []wgkey.Key
	for k := range r.learned {
		if _, still := r.peers[k]; still {
			out = append(out, k)
		}
	}
	return out
}

// allow takes one request from the allowance of the paired device pub. Callers hold r.mu.
func (r *Responder) allow(pub wgkey.Key, now time.Time) bool {
	b := r.rates[pub]
	if b == nil {
		b = &rate{left: burst}
		r.rates[pub] = b
	} else {
		b.left = min(b.left+now.Sub(b.filled).Seconds()*perSec, burst)
	}
	b.filled = now
	if b.left < 1 {
		return false
	}
	b.left--
	return true
}

// Answer returns the answer to one request, or nil when there is none to give: a malformed request, one from
// no paired device, nothing to tell, or too many requests at once.
func (r *Responder) Answer(request []byte) []byte {
	if len(request) != requestLen || string(request[:len(magic)]) != magic {
		return nil
	}
	challenge := request[len(magic) : len(magic)+challengeLen]
	tag := request[len(magic)+challengeLen:]

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.info == nil {
		return nil
	}
	for pub, k := range r.peers {
		if !hmac.Equal(requestTag(k, challenge), tag) {
			continue
		}
		// Counted only now that the sender proved to be this device: anybody in reach of the phone's Wi-Fi could
		// otherwise use the allowance up with junk, and the paired devices would get no answer.
		if !r.allow(pub, time.Now()) {
			return nil
		}
		info := *r.info
		if r.devices[pub] {
			info.App, info.PC = info.App.forDevice(), info.PC.forDevice()
		}
		plain, err := json.Marshal(&info)
		if err != nil {
			return nil
		}
		r.asked[pub] = true
		if r.info.Next != nil {
			r.learned[pub] = true
		}
		nonce := make([]byte, nonceLen)
		if _, err := rand.Read(nonce); err != nil {
			return nil
		}
		out := append([]byte(magic), nonce...)
		return answerAEAD(k, challenge).Seal(out, nonce, plain, additional(challenge))
	}
	return nil
}

// Serve answers the requests that arrive on conn until it is closed. accept, if given, picks the senders
// that get an answer at all.
func (r *Responder) Serve(conn net.PacketConn, accept func(netip.Addr) bool) error {
	buf := make([]byte, maxRead)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return err
		}
		if accept != nil {
			ua, ok := from.(*net.UDPAddr)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(ua.IP)
			if !ok || !accept(ip.Unmap()) {
				continue
			}
		}
		if ans := r.Answer(buf[:n]); ans != nil {
			conn.WriteTo(ans, from)
		}
	}
}

// Ask asks the phone at addr, over conn, for its network. private is this device's WireGuard key and phone the
// phone's public key. It asks again while no answer comes, until ctx ends.
func Ask(ctx context.Context, conn net.PacketConn, addr net.Addr, private, phone wgkey.Key) (Info, error) {
	k, err := pairKey(private, phone)
	if err != nil {
		return Info{}, err
	}
	challenge := make([]byte, challengeLen)
	if _, err := rand.Read(challenge); err != nil {
		return Info{}, err
	}
	request := append(append([]byte(magic), challenge...), requestTag(k, challenge)...)
	aead := answerAEAD(k, challenge)

	buf := make([]byte, maxRead)
	for {
		if _, err := conn.WriteTo(request, addr); err != nil {
			return Info{}, err
		}
		deadline := time.Now().Add(retryAfter)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		conn.SetReadDeadline(deadline)
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					break
				}
				return Info{}, err
			}
			if info, ok := open(aead, challenge, buf[:n]); ok {
				return info, nil
			}
		}
		if d, ok := ctx.Deadline(); ctx.Err() != nil || (ok && !time.Now().Before(d)) {
			return Info{}, ErrNoAnswer
		}
	}
}

// open checks and reads one answer. Anything else that arrives (a stray packet, an answer to another
// challenge) is not an answer.
func open(aead cipher.AEAD, challenge, packet []byte) (Info, bool) {
	if len(packet) < len(magic)+nonceLen+aead.Overhead() || string(packet[:len(magic)]) != magic {
		return Info{}, false
	}
	nonce := packet[len(magic) : len(magic)+nonceLen]
	plain, err := aead.Open(nil, nonce, packet[len(magic)+nonceLen:], additional(challenge))
	if err != nil {
		return Info{}, false
	}
	var info Info
	if json.Unmarshal(plain, &info) != nil || !info.Current.Valid() || (info.Next != nil && !info.Next.Valid()) {
		return Info{}, false
	}
	return info, true
}
