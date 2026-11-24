package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"hotspot/core/internal/release"
)

//go:embed ui/index.html
var indexHTML string

// licensesText is the license texts of everything this program is built from (cmd/notices writes the file). The
// program carries them, as the licenses ask, and prints them with "licenses"; the window does not show them.
//
//go:embed ui/licenses.txt
var licensesText string

// errNoWebView means Windows lacks the Edge WebView2 runtime the window is drawn with (Windows 11 always has it).
var errNoWebView = errors.New("Microsoft Edge WebView2 bulunamadı")

// windowState is what the window shows; it is sent whole whenever it changes.
type windowState struct {
	Installed        bool         `json:"installed"`
	Elevated         bool         `json:"elevated"`
	Version          string       `json:"version"`          // this file's release
	InstalledVersion string       `json:"installedVersion"` // the installed program's, "" when unknown
	CanUpgrade       bool         `json:"canUpgrade"`       // this file is not the installed program
	Name             string       `json:"name"`             // this computer, as the phone lists it
	Network          string       `json:"network"`
	Next             string       `json:"next"`
	AutoJoin         bool         `json:"autoJoin"`
	OnNetwork        bool         `json:"onNetwork"`
	Tunnel           string       `json:"tunnel"`
	PhoneApp         int          `json:"phoneApp"`     // the newest app the phone had when last asked (0: not known)
	PhoneAppName     string       `json:"phoneAppName"` // what that release is called
	PhoneWaiting     bool         `json:"phoneWaiting"` // the phone has that release but still runs an older one
	Phone            *phoneStatus `json:"phone"`        // the phone's state now; nil when it cannot be asked
	View             connView     `json:"view"`
	Log              []string     `json:"log"`
	Online           onlineOffer  `json:"online"`        // a newer release on GitHub; its name is empty when there is none
	OnlineChecked    string       `json:"onlineChecked"` // when GitHub last answered, as the window writes it; "" for never
}

// onlineOffer is the newer release the window offers to install.
type onlineOffer struct {
	Name string `json:"name"`
	Size int64  `json:"size"` // of the Windows program, in bytes
}

// event is what the Go side tells the page as it happens.
type event struct {
	Type    string       `json:"type"` // state, step, done, password, choose, qr, number, finished, failed, cancelled, action
	Step    stepID       `json:"step,omitempty"`
	Detail  string       `json:"detail,omitempty"`
	SSID    string       `json:"ssid,omitempty"`
	Retry   bool         `json:"retry,omitempty"`
	Options []string     `json:"options,omitempty"`
	SVG     string       `json:"svg,omitempty"`
	Action  string       `json:"action,omitempty"`
	Error   string       `json:"error,omitempty"`
	State   *windowState `json:"state,omitempty"`
	Speed   *speedReport `json:"speed,omitempty"`
}

type answer struct {
	value string
	ok    bool
}

type window struct {
	w        webview2.WebView
	answers  chan answer
	busy     atomic.Bool // a setup or an action runs; only one at a time
	checking atomic.Bool // GitHub is being asked because the user said so
	upgrade  bool        // this file differs from the installed program (worked out once: hashing is not free)
	mu       sync.Mutex
	last     windowState
	setupNow bool
}

// runWindow opens the program's window. setupNow starts the setup at once: the window was reopened with
// administrator rights for it.
func runWindow(setupNow bool) error {
	scale := dpiScale()
	// WebView2 refuses a data folder that a browser process of another rights level still holds, and the window
	// reopened with administrator rights starts while the one before is closing.
	data := "WebView2"
	if isElevated() {
		data = "WebView2-admin"
	}
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  filepath.Join(os.Getenv("LOCALAPPDATA"), "Hotspot", data),
		WindowOptions: webview2.WindowOptions{
			Title: "AleShare", Width: uint(480 * scale), Height: uint(780 * scale), IconId: 1, Center: true,
		},
	})
	if w == nil {
		return errNoWebView
	}
	defer w.Destroy()
	w.SetSize(int(420*scale), int(600*scale), webview2.HintMin)
	darkTitleBar(w)

	g := &window{w: w, answers: make(chan answer), setupNow: setupNow}
	g.upgrade = differs(installedExe())
	g.last = g.collect()
	for name, fn := range map[string]any{
		"hsBoot":        g.boot,
		"hsStartSetup":  g.startSetup,
		"hsAnswer":      g.answer,
		"hsSetAutoJoin": g.setAutoJoin,
		"hsUpgrade":     g.upgradeNow,
		"hsUninstall":   g.uninstall,
		"hsConnect":     g.connect,
		"hsOpenLog":     g.openLog,
		"hsPhone":       g.phone,
		"hsSpeedTest":   g.speedTest,
		"hsOnline":      g.installOnline,
		"hsCheckOnline": g.checkNow,
	} {
		if err := w.Bind(name, fn); err != nil {
			return err
		}
	}
	w.SetHtml(indexHTML)
	go g.watch()
	go func() { // is a newer release out? Asked when the window opens, at most once an hour.
		checkOnline(time.Since(loadOnline().Checked) > time.Hour)
		g.refresh()
	}()
	go startTray() // an installation that came by an upgrade gets its icon now, not only at the next sign-in
	w.Run()
	return nil
}

// boot is the page's first question: the state, and whether to start the setup right away.
func (g *window) boot() map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.setupNow && g.busy.CompareAndSwap(false, true) {
		go g.runSetup()
		return map[string]any{"state": g.last, "setup": true}
	}
	return map[string]any{"state": g.last, "setup": false}
}

func (g *window) emit(e event) {
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	g.w.Dispatch(func() { g.w.Eval("window.hsEvent(" + string(data) + ")") })
}

// watch keeps the shown state fresh: a cheap look every three seconds, sent only when something changed.
func (g *window) watch() {
	for {
		time.Sleep(3 * time.Second)
		g.refresh()
	}
}

func (g *window) refresh() {
	s := g.collect()
	g.mu.Lock()
	same := fmt.Sprint(s) == fmt.Sprint(g.last)
	g.last = s
	g.mu.Unlock()
	if !same {
		g.emit(event{Type: "state", State: &s})
	}
}

func (g *window) collect() windowState {
	s := windowState{Elevated: isElevated(), Version: version, Name: defaultName(), CanUpgrade: g.upgrade}
	set, err := loadSettings()
	if err != nil {
		return s
	}
	s.Installed = true
	s.InstalledVersion, s.Network, s.Next, s.AutoJoin = set.Version, set.Profile, set.Next, set.AutoJoin
	for _, ssid := range currentSSIDs() {
		s.OnNetwork = s.OnNetwork || knownNetwork(set, ssid)
	}
	s.Tunnel = serviceState()
	if rel := loadReleases(); rel.PhoneApp > 0 {
		s.PhoneApp, s.PhoneAppName = rel.PhoneApp, rel.PhoneAppName
		if s.PhoneAppName == "" {
			s.PhoneAppName = release.Name(s.PhoneApp)
		}
	}
	online := loadOnline()
	if rel := online.offer(installedCode(set)); rel != nil && !g.upgrade {
		s.Online = onlineOffer{Name: rel.Name, Size: rel.PC.Size}
	}
	if !online.Checked.IsZero() {
		s.OnlineChecked = online.Checked.Format("02.01.2006 15:04")
	}
	if s.OnNetwork {
		s.Phone, _ = fetchPhone(set, s.Tunnel == serviceRunning)
	}
	if s.Phone != nil {
		// The release the phone runs: its number when the app says it, else worked out from the name.
		running := s.Phone.Code
		if running == 0 {
			running = versionCode(s.Phone.Version)
		}
		s.PhoneWaiting = running > 0 && s.PhoneApp > running
	}
	s.View = describe(set, s.OnNetwork, s.Tunnel, loadHealth())
	s.Log = lastLog(6)
	return s
}

// ---- the setup ----

// startSetup begins the setup; without administrator rights the window first reopens with them (Windows asks).
func (g *window) startSetup() error {
	if !isElevated() {
		if err := shellRun("runas", "window -setup", windows.SW_NORMAL); err != nil {
			return errors.New("Yönetici izni verilmedi; kurulum için gerekli.")
		}
		g.w.Dispatch(g.w.Terminate)
		return nil
	}
	if !g.busy.CompareAndSwap(false, true) {
		return errors.New("Başka bir işlem sürüyor.")
	}
	go g.runSetup()
	return nil
}

func (g *window) runSetup() {
	defer g.busy.Store(false)
	err := doSetup(setupOptions{name: defaultName()}, windowUI{g})
	g.upgrade = differs(installedExe())
	switch {
	case errors.Is(err, errCancelled):
		g.emit(event{Type: "cancelled"})
	case err != nil:
		g.emit(event{Type: "failed", Error: err.Error()})
	default:
		g.emit(event{Type: "finished"})
	}
	g.refresh()
}

// answer takes what the user typed or picked; ok is false when they gave up.
func (g *window) answer(value string, ok bool) {
	select {
	case g.answers <- answer{value, ok}:
	default: // nobody is asking
	}
}

// windowUI is the setup shown in the window.
type windowUI struct{ g *window }

func (u windowUI) Step(id stepID, detail string) {
	u.g.emit(event{Type: "step", Step: id, Detail: detail})
}

func (u windowUI) Done(id stepID, detail string) {
	u.g.emit(event{Type: "done", Step: id, Detail: detail})
}

func (u windowUI) Password(ssid string, retry bool) (string, bool) {
	u.g.emit(event{Type: "password", Step: stepWifi, SSID: ssid, Retry: retry})
	a := <-u.g.answers
	return a.value, a.ok
}

func (u windowUI) Choose(question string, options []string) string {
	u.g.emit(event{Type: "choose", Step: stepWifi, Detail: question, Options: options})
	a := <-u.g.answers
	for _, o := range options {
		if a.ok && a.value == o {
			return o
		}
	}
	return options[0]
}

func (u windowUI) QR(code string) {
	svg, err := qrSVG(code)
	if err != nil {
		u.g.emit(event{Type: "failed", Error: err.Error()})
		return
	}
	u.g.emit(event{Type: "qr", Step: stepPair, SVG: svg})
}

func (u windowUI) Number(sas string) { u.g.emit(event{Type: "number", Step: stepPair, Detail: sas}) }

func (u windowUI) Out() io.Writer { return io.Discard }

// ---- what the installed program offers ----

// act runs one of the commands that need administrator rights, in the background, and tells the page how it went.
func (g *window) act(name string, rest ...string) error {
	if !g.busy.CompareAndSwap(false, true) {
		return errors.New("Başka bir işlem sürüyor.")
	}
	go func() {
		defer g.busy.Store(false)
		err := runElevated(name, rest...)
		if name == "upgrade" && err == nil {
			g.upgrade = differs(installedExe())
		}
		g.refresh() // first, so that the page's message reads the new state
		e := event{Type: "action", Action: name}
		if err != nil {
			e.Error = err.Error()
		}
		g.emit(e)
	}()
	return nil
}

func (g *window) setAutoJoin(on bool) error { return g.act("autojoin", onOffArg(on)) }

func (g *window) upgradeNow() error { return g.act("upgrade") }

// installedCode is the release number of the installed program (this program's own when the settings do not say).
func installedCode(set settings) int {
	if n := versionCode(set.Version); n > 0 {
		return n
	}
	return versionCode(version)
}

// installOnline downloads the newer release from GitHub, checks its signature and installs it (Windows asks for
// administrator rights). The window still runs the old program then, so it opens the new one and closes.
func (g *window) installOnline() error {
	set, err := loadSettings()
	if err != nil {
		return err
	}
	rel := loadOnline().offer(installedCode(set))
	if rel == nil {
		return errors.New("Yeni sürüm yok.")
	}
	if !g.busy.CompareAndSwap(false, true) {
		return errors.New("Başka bir işlem sürüyor.")
	}
	go func() {
		path, err := fetchOnlineProgram(*rel)
		if err == nil {
			err = runElevated("upgrade", "-from", path)
			_ = os.Remove(path)
		}
		g.busy.Store(false)
		g.refresh()
		e := event{Type: "action", Action: "online"}
		if err != nil {
			e.Error = err.Error()
			g.emit(e)
			return
		}
		g.emit(e)
		time.Sleep(1500 * time.Millisecond) // long enough to read "Güncellendi"
		if exec.Command(installedExe()).Start() == nil {
			g.w.Dispatch(g.w.Terminate)
		}
	}()
	return nil
}

func (g *window) uninstall() error { return g.act("uninstall") }

// checkNow asks GitHub at once whether a newer release is out (the window and the icon otherwise look every few
// hours) and tells the page what came of it; a newer release then shows with its button, as it does by itself.
func (g *window) checkNow() error {
	set, err := loadSettings()
	if err != nil {
		return err
	}
	if !g.checking.CompareAndSwap(false, true) {
		return errors.New("Zaten bakılıyor.")
	}
	go func() {
		defer g.checking.Store(false)
		began := time.Now()
		text, answered := lookResult(checkOnline(true), began, installedCode(set))
		g.refresh() // first, so that the release is on the page when the message comes
		e := event{Type: "action", Action: "check", Detail: text}
		if !answered {
			e.Detail, e.Error = "", text
		}
		g.emit(e)
	}()
	return nil
}

// connect asks Windows to join the phone's network now; the tunnel follows by itself. No rights needed.
func (g *window) connect() error {
	set, err := loadSettings()
	if err != nil {
		return err
	}
	go func() {
		_ = run("netsh.exe", "wlan", "connect", "name="+set.Profile, "ssid="+set.Profile)
		g.refresh()
	}()
	return nil
}

// phone asks the phone for a change of its switches or of a device; the phone applies it within a few seconds, and
// the window shows it once it did. Only the keys the window offers go out.
func (g *window) phone(change map[string]any) error {
	allowed := map[string]bool{"split": true, "ads": true, "saver": true, "priority": true, "guard": true, "limit_mb": true, "pause": true, "resume": true}
	for k := range change {
		if !allowed[k] {
			return fmt.Errorf("bilinmeyen ayar %q", k)
		}
	}
	set, err := loadSettings()
	if err != nil {
		return err
	}
	go func() {
		err := sendPhone(set, serviceState() == serviceRunning, change)
		if err != nil {
			g.emit(event{Type: "action", Action: "phone", Error: err.Error()})
			return
		}
		time.Sleep(3500 * time.Millisecond) // the phone looks every three seconds
		g.refresh()
		g.emit(event{Type: "action", Action: "phone"})
	}()
	return nil
}

// speedReport is how far the speed test got: the Wi-Fi between this computer and the phone first, then the phone's
// mobile internet, which the phone measures itself. Rates in bits per second.
type speedReport struct {
	Stage    string      `json:"stage"` // "wifi", "mobile" or "done"
	WifiDown int64       `json:"wifiDown"`
	WifiUp   int64       `json:"wifiUp"`
	Mobile   *phoneSpeed `json:"mobile,omitempty"`
	Error    string      `json:"error,omitempty"`
}

// speedTest measures the Wi-Fi to the phone and has the phone measure its mobile internet; the page hears each
// stage as a "speed" event, so it can tell where the bottleneck is.
func (g *window) speedTest() error {
	set, err := loadSettings()
	if err != nil {
		return err
	}
	if !g.busy.CompareAndSwap(false, true) {
		return errors.New("Başka bir işlem sürüyor.")
	}
	go func() {
		defer g.busy.Store(false)
		via := serviceState() == serviceRunning
		rep := speedReport{Stage: "wifi"}
		report := func() {
			r := rep
			g.emit(event{Type: "speed", Speed: &r})
		}
		report()
		down, up, err := measureWifi(set, via)
		if err != nil {
			rep.Stage, rep.Error = "done", err.Error()
			report()
			return
		}
		rep.WifiDown, rep.WifiUp, rep.Stage = down, up, "mobile"
		report()
		var before int64
		if p, _ := fetchPhone(set, via); p != nil && p.SpeedTest != nil {
			before = p.SpeedTest.At
		}
		if err := sendPhone(set, via, map[string]any{"speedtest": true}); err != nil {
			rep.Stage, rep.Error = "done", err.Error()
			report()
			return
		}
		asked := time.Now()
		for time.Since(asked) < 75*time.Second {
			time.Sleep(2 * time.Second)
			p, err := fetchPhone(set, via)
			if err != nil || p.SpeedTest == nil || p.SpeedTest.Running {
				continue
			}
			if p.SpeedTest.At > before {
				rep.Mobile = p.SpeedTest
				break
			}
			// The phone looks for requests every three seconds: an error before then is an older one.
			if p.SpeedTest.Error != "" && time.Since(asked) > 8*time.Second {
				rep.Error = p.SpeedTest.Error
				break
			}
		}
		if rep.Mobile == nil && rep.Error == "" {
			rep.Error = "Telefon mobil hızı ölçemedi (telefonda AleShare 1.0.18 ya da sonrası gerekir)."
		}
		rep.Stage = "done"
		report()
	}()
	return nil
}

func (g *window) openLog() error {
	return exec.Command("notepad.exe", filepath.Join(dataDir(), "hotspot.log")).Start()
}

func onOffArg(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// ---- Windows ----

// serviceState looks at the tunnel service with the least rights that allow it, so the window needs none.
func serviceState() string {
	m, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return serviceStopped
	}
	defer windows.CloseServiceHandle(m)
	s, err := windows.OpenService(m, windows.StringToUTF16Ptr(serviceName), windows.SERVICE_QUERY_STATUS)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return serviceMissing
	}
	if err != nil {
		return serviceStopped
	}
	defer windows.CloseServiceHandle(s)
	var st windows.SERVICE_STATUS
	if windows.QueryServiceStatus(s, &st) != nil {
		return serviceStopped
	}
	if st.CurrentState == windows.SERVICE_RUNNING || st.CurrentState == windows.SERVICE_START_PENDING {
		return serviceRunning
	}
	return serviceStopped
}

// lastLog is the end of the program's log, newest last.
func lastLog(n int) []string {
	data, err := os.ReadFile(filepath.Join(dataDir(), "hotspot.log"))
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return lines
}

// differs reports whether this program is another file than path (missing counts as other): then installing it
// means something.
func differs(path string) bool {
	self, err := os.Executable()
	if err != nil || strings.EqualFold(self, path) {
		return false
	}
	a, err1 := os.ReadFile(self)
	b, err2 := os.ReadFile(path)
	return err1 != nil || err2 != nil || string(a) != string(b)
}

var (
	shell32           = windows.NewLazySystemDLL("shell32.dll")
	shellExecuteExW   = shell32.NewProc("ShellExecuteExW")
	user32            = windows.NewLazySystemDLL("user32.dll")
	getDpiForSystem   = user32.NewProc("GetDpiForSystem")
	errRightsRefused  = errors.New("Yönetici izni verilmedi.")
	errActionNoAnswer = errors.New("İşlem tamamlanamadı.")
)

// shellExecuteInfo is SHELLEXECUTEINFOW.
type shellExecuteInfo struct {
	size       uint32
	mask       uint32
	hwnd       uintptr
	verb       *uint16
	file       *uint16
	parameters *uint16
	directory  *uint16
	show       int32
	instApp    uintptr
	idList     uintptr
	class      *uint16
	keyClass   uintptr
	hotKey     uint32
	icon       uintptr
	process    windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x40
	seeMaskNoAsync        = 0x100
)

// shellStart starts this program again through the shell, with administrator rights when verb is "runas"
// (Windows asks the user); the process handle comes back when wait is set.
func shellStart(verb, params string, show int32, wait bool) (windows.Handle, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	info := shellExecuteInfo{
		mask:       seeMaskNoAsync,
		verb:       windows.StringToUTF16Ptr(verb),
		file:       windows.StringToUTF16Ptr(exe),
		parameters: windows.StringToUTF16Ptr(params),
		directory:  windows.StringToUTF16Ptr(filepath.Dir(exe)),
		show:       show,
	}
	if wait {
		info.mask |= seeMaskNoCloseProcess
	}
	info.size = uint32(unsafe.Sizeof(info))
	if r, _, e := shellExecuteExW.Call(uintptr(unsafe.Pointer(&info))); r == 0 {
		if errors.Is(e, windows.ERROR_CANCELLED) {
			return 0, errRightsRefused
		}
		return 0, e
	}
	return info.process, nil
}

func shellRun(verb, params string, show int32) error {
	_, err := shellStart(verb, params, show, false)
	return err
}

// runElevated runs a command of this program (autojoin, upgrade, uninstall) with administrator rights and no
// window of its own, and waits for it. The command writes its error, if any, to a file the window reads (see
// quietFlags). A window that has the rights already runs it itself.
func runElevated(cmd string, rest ...string) error {
	if isElevated() {
		return quietCommand(cmd, append([]string{"-quiet"}, rest...))
	}
	result := filepath.Join(os.TempDir(), fmt.Sprintf("hotspot-result-%d.txt", time.Now().UnixNano()))
	defer os.Remove(result)
	var params []string
	for _, a := range append([]string{cmd, "-quiet", "-result", result}, rest...) { // flags before the words
		params = append(params, syscall.EscapeArg(a))
	}
	h, err := shellStart("runas", strings.Join(params, " "), windows.SW_HIDE, true)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	if _, err := windows.WaitForSingleObject(h, windows.INFINITE); err != nil {
		return err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return err
	}
	if code != 0 {
		if msg, err := os.ReadFile(result); err == nil && len(msg) > 0 {
			return errors.New(strings.TrimSpace(string(msg)))
		}
		return errActionNoAnswer
	}
	return nil
}

// dpiScale is the display's scale (1.5 at 150 %): the window is sized in real pixels.
func dpiScale() float64 {
	if getDpiForSystem.Find() != nil {
		return 1
	}
	dpi, _, _ := getDpiForSystem.Call()
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / 96
}

// darkTitleBar gives the window a dark title bar when Windows' apps are in dark mode, as the page then is.
func darkTitleBar(w webview2.WebView) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	if light, _, err := k.GetIntegerValue("AppsUseLightTheme"); err != nil || light != 0 {
		return
	}
	on := uint32(1)
	_ = windows.DwmSetWindowAttribute(windows.HWND(w.Window()), windows.DWMWA_USE_IMMERSIVE_DARK_MODE, unsafe.Pointer(&on), 4)
}
