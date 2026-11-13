package wgcore

import (
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"hotspot/core/internal/release"
)

// Updates move through the sharing phone. It keeps the newest app (APK) and the newest Windows program it knows
// of, hands each to paired devices that run an older one, and takes a newer one from a paired device that brings
// it: whichever device was updated first, the others follow.
//
// Only paired devices take part: they learn the phone's token in the sealed answer about the Wi-Fi network, and
// every request needs it. A Windows program is taken only with this project's release signature (see
// internal/release), which is also what the computers check before they install one. An APK is checked by the app
// (its signing certificate must be this app's) before the phone offers it, and Android checks it again on install.
//
//	GET  /hotspot.apk?t=TOKEN               the phone's APK
//	POST /hotspot.apk?t=TOKEN&code=N        a newer APK (release N) from a device
//	GET  /hotspot-pc.exe?t=TOKEN            the Windows program
//	POST /hotspot-pc.exe?t=TOKEN            a newer Windows program from a computer
//	GET  /phone?t=TOKEN                     the phone's state as the app tells it (SetPhoneStatus), JSON
//	POST /phone?t=TOKEN                     a change a computer asks for, a JSON object (TakeControls)

// UpdatePort is where the phone hands out and takes releases: on its Wi-Fi network, and on its own address inside
// the tunnel (a computer whose tunnel is up can reach nothing else).
const UpdatePort = 51823

const (
	maxAPK     = 200 << 20
	maxProgram = 64 << 20
)

// tunnelNets are the paired devices' addresses inside the tunnel.
var tunnelNets = []netip.Prefix{netip.MustParsePrefix("10.8.0.0/24"), netip.MustParsePrefix("fd42:42:42::/64")}

// release is one file on offer.
type releaseFile struct {
	path string
	code int
	name string
}

// updates is the phone's store of releases.
type updates struct {
	mu       sync.Mutex
	token    string
	app, pc  releaseFile
	store    string       // where brought files go; empty: nothing is taken
	received *releaseFile // an APK a device brought, until the app has looked at it
	verify   func([]byte) (release.Info, error)
	changed  func() // what is on offer changed
	status   string   // the phone's state for the computers, JSON (SetPhoneStatus)
	controls []string // changes the computers asked for, JSON objects, until the app takes them
}

const (
	maxControl  = 4 << 10
	maxControls = 32
)

func newUpdates() *updates {
	return &updates{token: randomToken(), verify: release.Verify, changed: func() {}}
}

func (u *updates) offers() (app, pc releaseFile, token string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.app, u.pc, u.token
}

func (u *updates) setApp(f releaseFile) {
	u.mu.Lock()
	u.app = f
	u.mu.Unlock()
	u.changed()
}

// setProgram offers a signed Windows program if it is newer than the one on offer.
func (u *updates) setProgram(path string) (release.Info, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return release.Info{}, err
	}
	info, err := u.verify(data)
	if err != nil {
		return release.Info{}, err
	}
	u.mu.Lock()
	newer := info.Code > u.pc.code
	if newer {
		u.pc = releaseFile{path, info.Code, info.Name}
	}
	u.mu.Unlock()
	if newer {
		u.changed()
	}
	return info, nil
}

// takeReceived hands over the APK a device brought (once).
func (u *updates) takeReceived() *releaseFile {
	u.mu.Lock()
	defer u.mu.Unlock()
	r := u.received
	u.received = nil
	return r
}

// handler serves the store to senders that accept likes and that know the token; anything else is not found.
func (u *updates) handler(accept func(netip.Addr) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app, pc, token := u.offers()
		ip, err := netip.ParseAddrPort(r.RemoteAddr)
		if err != nil || !accept(ip.Addr().Unmap()) || subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("t")), []byte(token)) != 1 {
			http.NotFound(w, r)
			return
		}
		switch {
		case r.URL.Path == "/hotspot.apk" && r.Method == http.MethodGet && app.path != "":
			w.Header().Set("Content-Type", "application/vnd.android.package-archive")
			http.ServeFile(w, r, app.path)
		case r.URL.Path == "/hotspot-pc.exe" && r.Method == http.MethodGet && pc.path != "":
			w.Header().Set("Content-Type", "application/octet-stream")
			http.ServeFile(w, r, pc.path)
		case r.URL.Path == "/hotspot.apk" && r.Method == http.MethodPost:
			u.receiveAPK(w, r)
		case r.URL.Path == "/hotspot-pc.exe" && r.Method == http.MethodPost:
			u.receiveProgram(w, r)
		case r.URL.Path == "/phone" && r.Method == http.MethodGet:
			u.mu.Lock()
			status := u.status
			u.mu.Unlock()
			if status == "" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, status)
		case r.URL.Path == "/phone" && r.Method == http.MethodPost:
			u.receiveControl(w, r)
		case r.URL.Path == "/speed" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
			serveSpeed(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// receiveAPK keeps an APK a device brings when it says it is a newer release than the phone has; the app checks
// that claim (and the signing certificate) before anything is offered.
func (u *updates) receiveAPK(w http.ResponseWriter, r *http.Request) {
	code, _ := strconv.Atoi(r.URL.Query().Get("code"))
	u.mu.Lock()
	store, have := u.store, u.app.code
	if u.received != nil && u.received.code > have {
		have = u.received.code
	}
	u.mu.Unlock()
	switch {
	case store == "":
		http.Error(w, "no store", http.StatusServiceUnavailable)
		return
	case code <= have:
		http.Error(w, "not newer", http.StatusConflict)
		return
	}
	path := filepath.Join(store, fmt.Sprintf("received-%d.apk", code))
	if err := saveBody(r.Body, path, maxAPK); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	u.mu.Lock()
	u.received = &releaseFile{path, code, release.Name(code)}
	u.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

// receiveProgram keeps a Windows program a computer brings when it carries the release signature and is newer.
func (u *updates) receiveProgram(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	store := u.store
	u.mu.Unlock()
	if store == "" {
		http.Error(w, "no store", http.StatusServiceUnavailable)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxProgram+1))
	if err != nil || len(data) > maxProgram {
		http.Error(w, "too big", http.StatusRequestEntityTooLarge)
		return
	}
	info, err := u.verify(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	u.mu.Lock()
	newer := info.Code > u.pc.code
	u.mu.Unlock()
	if !newer {
		http.Error(w, "not newer", http.StatusConflict)
		return
	}
	path := filepath.Join(store, "hotspot-pc.exe")
	if err := writeAtomic(path, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := u.setProgram(path); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// receiveControl queues a change a computer asks for: one JSON object of plain values. The app decides what it means
// (and ignores what it does not know).
func (u *updates) receiveControl(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxControl+1))
	var obj map[string]any
	if err != nil || len(data) > maxControl || json.Unmarshal(data, &obj) != nil || len(obj) == 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	for _, v := range obj {
		switch v.(type) {
		case bool, float64, string:
		default:
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.controls) >= maxControls {
		http.Error(w, "busy", http.StatusTooManyRequests)
		return
	}
	u.controls = append(u.controls, string(data))
	w.WriteHeader(http.StatusOK)
}

// saveBody writes a request's body to path, through a temporary file, refusing more than max bytes.
func saveBody(body io.Reader, path string, max int64) error {
	part := path + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(body, max+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > max {
		err = fmt.Errorf("more than %d bytes", max)
	}
	if err != nil {
		os.Remove(part)
		return err
	}
	return os.Rename(part, path)
}

func writeAtomic(path string, data []byte) error {
	part := path + ".part"
	if err := os.WriteFile(part, data, 0o644); err != nil {
		return err
	}
	return os.Rename(part, path)
}

// tunnelListener hands the update server the connections that paired devices open through the tunnel.
type tunnelListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newTunnelListener() *tunnelListener {
	return &tunnelListener{conns: make(chan net.Conn), done: make(chan struct{})}
}

// take is the gateway's TCP service for UpdatePort.
func (l *tunnelListener) take(c net.Conn) {
	select {
	case l.conns <- c:
	case <-l.done:
		c.Close()
	}
}

func (l *tunnelListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *tunnelListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *tunnelListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(10, 8, 0, 1), Port: UpdatePort} }

// ---- the core's side ----

// SetAppUpdate offers an app (APK) to the paired devices: the phone's own (path is the installed APK) or a newer
// one a device brought, once the app has checked it. code and name are its release. Devices with an older one
// download it; Android installs it only over an app signed with the same key. It takes effect at once.
func (c *Core) SetAppUpdate(path string, code int, name string) {
	c.upd.setApp(releaseFile{path, code, name})
}

// SetUpdateStore names the folder where the releases devices bring are kept; a Windows program kept there
// before is offered again.
// SetUpdateToken sets the token paired devices use with the store; they learn it from the sealed answer about the
// Wi-Fi network. The app keeps it across its restarts (an update, Android ending the process), so that a computer's
// window goes on reading the phone at once instead of a minute later, and gives a new one when a device is removed.
// A token that is not 32 hex digits is ignored; without one, each start makes its own.
func (c *Core) SetUpdateToken(token string) {
	if !validToken(token) {
		return
	}
	c.upd.mu.Lock()
	c.upd.token = token
	c.upd.mu.Unlock()
	c.publish()
}

func validToken(t string) bool {
	if len(t) != 32 {
		return false
	}
	_, err := hex.DecodeString(t)
	return err == nil
}

func (c *Core) SetUpdateStore(dir string) {
	c.upd.mu.Lock()
	c.upd.store = dir
	c.upd.mu.Unlock()
	if dir != "" {
		_, _ = c.upd.setProgram(filepath.Join(dir, "hotspot-pc.exe"))
	}
}

// SetPCProgram offers a Windows program (the one that came inside the app) if it carries the release signature
// and is newer than the one on offer. It returns what its release is called.
func (c *Core) SetPCProgram(path string) (string, error) {
	info, err := c.upd.setProgram(path)
	return info.Name, err
}

// ReceivedApp returns an APK a device brought since the last call, as "path<TAB>release number", or "" when there
// is none. The app checks it (signing certificate, version) and offers it with SetAppUpdate if it is good.
func (c *Core) ReceivedApp() string {
	r := c.upd.takeReceived()
	if r == nil {
		return ""
	}
	return r.path + "\t" + strconv.Itoa(r.code)
}

// SetPhoneStatus sets what paired computers are told about the phone (battery, mobile network, data used, the
// switches…), as a JSON object; the app refreshes it every few seconds while sharing runs.
func (c *Core) SetPhoneStatus(json string) {
	c.upd.mu.Lock()
	c.upd.status = json
	c.upd.mu.Unlock()
}

// TakeControls returns the changes paired computers asked for since the last call, one JSON object per line ("" when
// none). The app checks each key and value before it applies anything.
func (c *Core) TakeControls() string {
	c.upd.mu.Lock()
	defer c.upd.mu.Unlock()
	out := strings.Join(c.upd.controls, "\n")
	c.upd.controls = nil
	return out
}

// acceptUpdate tells who may use the store: devices on the phone's Wi-Fi network and, through the tunnel, paired
// devices (the token is checked on top).
func acceptUpdate(a netip.Addr) bool {
	if wifiDirect.Contains(a) {
		return true
	}
	for _, p := range tunnelNets {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// startUpdates serves the store on the Wi-Fi network and inside the tunnel. Callers hold c.mu.
func (c *Core) startUpdates() map[uint16]func(net.Conn) {
	srv := &http.Server{Handler: c.upd.handler(acceptUpdate), ReadHeaderTimeout: 10 * time.Second}
	c.updateTunnel = newTunnelListener()
	go srv.Serve(c.updateTunnel)
	if ln, err := net.Listen("tcp4", fmt.Sprintf(":%d", UpdatePort)); err == nil {
		c.updateLn = ln
		go srv.Serve(ln)
	}
	c.updateSrv = srv
	return map[uint16]func(net.Conn){UpdatePort: c.updateTunnel.take}
}

func (c *Core) stopUpdates() {
	if c.updateSrv != nil {
		c.updateSrv.Close() // closes both listeners
		c.updateSrv, c.updateLn = nil, nil
	}
	if c.updateTunnel != nil {
		c.updateTunnel.Close()
		c.updateTunnel = nil
	}
}
