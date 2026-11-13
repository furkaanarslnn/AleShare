package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The tray icon: next to the clock, for as long as the user is signed in (started by the Run key, see
// installTrayAutostart). It shows whether the tunnel is up and how the phone is doing, tells of what needs the
// user's eye (the connection dropped, the phone's battery is low or it got hot, a limit is near, this computer's
// internet was paused), and opens the window. It asks the same things the window does, every few seconds, without
// administrator rights.

const (
	trayClass     = "HotspotTray"
	trayMutex     = `Local\HotspotTray`
	trayEvery     = 5 * time.Second
	runKey        = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue      = "Hotspot"
	wmTray        = 0x8000 + 1 // WM_APP + 1: the icon's mouse events
	wmClose       = 0x0010
	wmDestroy     = 0x0002
	wmCommand     = 0x0111
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	ninBalloonHit = 0x0405 // NIN_BALLOONUSERCLICK
	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nifMessage    = 0x01
	nifIcon       = 0x02
	nifTip        = 0x04
	nifInfo       = 0x10
	niifInfo      = 0x01
	niifWarning   = 0x02
	menuOpen      = 1
	menuJoin      = 2
	menuQuit      = 9
)

var (
	shellNotifyIcon       = shell32.NewProc("Shell_NotifyIconW")
	registerClassEx       = user32.NewProc("RegisterClassExW")
	createWindowEx        = user32.NewProc("CreateWindowExW")
	defWindowProc         = user32.NewProc("DefWindowProcW")
	getMessage            = user32.NewProc("GetMessageW")
	translateMessage      = user32.NewProc("TranslateMessage")
	dispatchMessage       = user32.NewProc("DispatchMessageW")
	postQuitMessage       = user32.NewProc("PostQuitMessage")
	loadImage             = user32.NewProc("LoadImageW")
	getSystemMetrics      = user32.NewProc("GetSystemMetrics")
	createPopupMenu       = user32.NewProc("CreatePopupMenu")
	appendMenu            = user32.NewProc("AppendMenuW")
	trackPopupMenu        = user32.NewProc("TrackPopupMenu")
	destroyMenu           = user32.NewProc("DestroyMenu")
	setForegroundWindow   = user32.NewProc("SetForegroundWindow")
	getCursorPos          = user32.NewProc("GetCursorPos")
	postMessage           = user32.NewProc("PostMessageW")
	findWindow            = user32.NewProc("FindWindowW")
	registerWindowMessage = user32.NewProc("RegisterWindowMessageW")
	getModuleHandle       = kernel32.NewProc("GetModuleHandleW")
)

// notifyIconData is NOTIFYICONDATAW.
type notifyIconData struct {
	size            uint32
	wnd             windows.HWND
	id              uint32
	flags           uint32
	callbackMessage uint32
	icon            windows.Handle
	tip             [128]uint16
	state           uint32
	stateMask       uint32
	info            [256]uint16
	timeout         uint32
	infoTitle       [64]uint16
	infoFlags       uint32
	guid            windows.GUID
	balloonIcon     windows.Handle
}

// wndClassEx is WNDCLASSEXW.
type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSm     windows.Handle
}

type winMsg struct {
	hwnd    windows.HWND
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

type tray struct {
	mu        sync.Mutex
	nid       notifyIconData
	icons     [2]windows.Handle // connected, not connected
	connected bool
	alerts    trayAlerts
	looking   atomic.Bool // a look at GitHub is under way
}

// cmdTray runs the tray icon until the user quits it or signs out; a second one quits at once. With -after it is
// the icon of a newer release taking over (see watch): it waits for the one before to quit.
func cmdTray(args []string) error {
	wait := time.Duration(0)
	if len(args) > 0 && args[0] == "-after" {
		wait = 15 * time.Second
	}
	mutex, ok := trayLock(wait)
	if !ok {
		return nil // one runs already
	}
	defer windows.CloseHandle(mutex)
	if _, err := loadSettings(); err != nil {
		return nil // not set up (any more)
	}
	runtime.LockOSThread()
	t := &tray{}
	instance, _, _ := getModuleHandle.Call(0)
	size, _, _ := getSystemMetrics.Call(49) // SM_CXSMICON
	for i, id := range []uintptr{1, 2} {
		h, _, _ := loadImage.Call(instance, id, 1 /* IMAGE_ICON */, size, size, 0)
		t.icons[i] = windows.Handle(h)
	}
	class, _ := windows.UTF16PtrFromString(trayClass)
	taskbarCreated, _, _ := registerWindowMessage.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("TaskbarCreated"))))
	var hwnd windows.HWND
	proc := windows.NewCallback(func(h windows.HWND, m uint32, wParam, lParam uintptr) uintptr {
		switch {
		case m == wmTray:
			switch lParam & 0xffff {
			case wmLButtonUp, ninBalloonHit:
				openWindow()
			case wmRButtonUp:
				t.menu(h)
			}
			return 0
		case m == wmClose:
			t.remove()
			postQuitMessage.Call(0)
			return 0
		case uintptr(m) == taskbarCreated && taskbarCreated != 0:
			t.add() // Explorer started again: the icon is gone with it
			return 0
		}
		r, _, _ := defWindowProc.Call(uintptr(h), uintptr(m), wParam, lParam)
		return r
	})
	wc := wndClassEx{wndProc: proc, instance: windows.Handle(instance), className: class}
	wc.size = uint32(unsafe.Sizeof(wc))
	registerClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	h, _, err := createWindowEx.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, 0, instance, 0)
	if h == 0 {
		return fmt.Errorf("tray window: %w", err)
	}
	hwnd = windows.HWND(h)
	t.nid.size = uint32(unsafe.Sizeof(t.nid))
	t.nid.wnd = hwnd
	t.nid.id = 1
	t.nid.callbackMessage = wmTray
	t.nid.icon = t.icons[1]
	setUTF16(t.nid.tip[:], "AleShare")
	t.add()
	go t.watch(hwnd)

	var m winMsg
	for {
		r, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&m)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

// trayLock makes this the one icon of the session, waiting up to wait for another one to quit. The mutex only
// tells that an icon runs: it lives as long as a handle to it is open.
func trayLock(wait time.Duration) (windows.Handle, bool) {
	name, _ := windows.UTF16PtrFromString(trayMutex)
	deadline := time.Now().Add(wait)
	for {
		h, err := windows.CreateMutex(nil, false, name)
		if err == nil {
			return h, true
		}
		if h != 0 {
			windows.CloseHandle(h) // or the other one's mutex would outlive it
		}
		if time.Now().After(deadline) {
			return 0, false
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func (t *tray) add() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nid.flags = nifMessage | nifIcon | nifTip
	shellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid)))
}

func (t *tray) remove() {
	t.mu.Lock()
	defer t.mu.Unlock()
	shellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&t.nid)))
}

// show sets the icon and the tooltip.
func (t *tray) show(connected bool, tip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.connected = connected
	t.nid.icon = t.icons[1]
	if connected {
		t.nid.icon = t.icons[0]
	}
	setUTF16(t.nid.tip[:], tip)
	t.nid.flags = nifIcon | nifTip
	shellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&t.nid)))
}

// notify shows a Windows notification from the icon; a click on it opens the window.
func (t *tray) notify(title, text string, warn bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	setUTF16(t.nid.infoTitle[:], title)
	setUTF16(t.nid.info[:], text)
	t.nid.infoFlags = niifInfo
	if warn {
		t.nid.infoFlags = niifWarning
	}
	t.nid.flags = nifInfo
	shellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&t.nid)))
}

func (t *tray) menu(hwnd windows.HWND) {
	m, _, _ := createPopupMenu.Call()
	if m == 0 {
		return
	}
	defer destroyMenu.Call(m)
	item := func(id uintptr, text string) {
		appendMenu.Call(m, 0, id, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(text))))
	}
	item(menuOpen, "AleShare'i aç")
	t.mu.Lock()
	connected := t.connected
	t.mu.Unlock()
	if !connected {
		item(menuJoin, "Telefonun ağına bağlan")
	}
	appendMenu.Call(m, 0x800 /* MF_SEPARATOR */, 0, 0)
	item(menuQuit, "Simgeyi kapat")
	var pt struct{ x, y int32 }
	getCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	setForegroundWindow.Call(uintptr(hwnd)) // or the menu stays open after a click elsewhere
	cmd, _, _ := trackPopupMenu.Call(m, 0x0100|0x0002 /* TPM_RETURNCMD | TPM_RIGHTBUTTON */, uintptr(pt.x), uintptr(pt.y), 0, uintptr(hwnd), 0)
	postMessage.Call(uintptr(hwnd), 0, 0, 0)
	switch cmd {
	case menuOpen:
		openWindow()
	case menuJoin:
		if set, err := loadSettings(); err == nil && set.Profile != "" {
			go func() { _ = run("netsh.exe", "wlan", "connect", "name="+set.Profile, "ssid="+set.Profile) }()
		}
	case menuQuit:
		postMessage.Call(uintptr(hwnd), wmClose, 0, 0)
	}
}

// watch looks at the tunnel and the phone every few seconds. When the installed program was replaced by a newer
// release, it hands over to it: the new one starts its own icon.
func (t *tray) watch(hwnd windows.HWND) {
	started := modTime(installedExe())
	for {
		set, err := loadSettings()
		if err != nil {
			postMessage.Call(uintptr(hwnd), wmClose, 0, 0) // uninstalled
			return
		}
		connected := serviceState() == serviceRunning
		var phone *phoneStatus
		if connected {
			phone, _ = fetchPhone(set, true)
		}
		t.show(connected, trayTip(connected, phone))
		for _, a := range t.alerts.check(connected, phone, defaultName(), time.Now()) {
			t.notify(a.title, a.text, a.warn)
		}
		t.lookOnline()
		time.Sleep(trayEvery)
		if now := modTime(installedExe()); !now.IsZero() && !now.Equal(started) {
			time.Sleep(3 * time.Second) // the upgrade may still be writing
			// The new icon first, waiting for this one to quit: once this one's window closes, the program ends.
			if exec.Command(installedExe(), "tray", "-after").Start() == nil {
				postMessage.Call(uintptr(hwnd), wmClose, 0, 0)
				return
			}
			started = now
		}
	}
}

// lookOnline asks GitHub now and then whether a newer release is out, and tells the user once about each.
func (t *tray) lookOnline() {
	s := loadOnline()
	if s.due(time.Now()) && t.looking.CompareAndSwap(false, true) {
		go func() {
			defer t.looking.Store(false)
			checkOnline(false)
		}()
	}
	if rel := s.announce(versionCode(version)); rel != nil {
		s.Told = rel.Code
		saveOnline(s)
		t.notify("AleShare "+rel.Name+" yayımlandı", "Güncellemek için AleShare penceresini aç ve Güncelle'ye tıkla.", false)
	}
}

func modTime(path string) time.Time {
	if fi, err := os.Stat(path); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}

func openWindow() { _ = exec.Command(installedExe()).Start() }

func setUTF16(dst []uint16, s string) {
	u, _ := windows.UTF16FromString(s)
	if len(u) > len(dst) {
		u = append(u[:len(dst)-1], 0)
	}
	clear(dst)
	copy(dst, u)
}

// trayTip is the tooltip: the connection, the speed now and the phone, in at most 127 characters.
func trayTip(connected bool, p *phoneStatus) string {
	if !connected {
		return "AleShare · bağlı değil"
	}
	if p == nil {
		return "AleShare · bağlı"
	}
	lines := []string{"AleShare · bağlı", "↓ " + rateText(p.DownBps*8) + "  ↑ " + rateText(p.UpBps*8)}
	phone := "Telefon"
	if p.Battery >= 0 {
		phone += fmt.Sprintf(" %%%d", p.Battery)
		if p.Charging {
			phone += " (şarjda)"
		}
	}
	if p.Network != "" {
		phone += " · " + p.Network
	}
	return strings.Join(append(lines, phone), "\n")
}

// rateText is bits per second the way people read it, with a decimal comma.
func rateText(bps int64) string {
	m := float64(bps) / 1e6
	if m >= 10 {
		return fmt.Sprintf("%.0f Mbit/s", m)
	}
	return strings.Replace(fmt.Sprintf("%.1f Mbit/s", m), ".", ",", 1)
}

// trayAlert is one notification to show.
type trayAlert struct {
	title, text string
	warn        bool
}

// trayAlerts remembers what was told already, so that each thing is told once until it is over.
type trayAlerts struct {
	seen        bool      // the first look only learns how things are
	wasUp       bool      // the connection was up at some time since the icon started: only then is its loss news
	downSince   time.Time // the tunnel went down at; zero while it is up
	toldDown    bool
	toldBattery bool
	toldHot     bool
	toldLimit   string // the day the daily limit's 90 % was told
	toldPlan    int    // the plan's share told (90, 100)
	toldPaused  string // why this computer's internet is paused, as told
}

// check compares how things are with how they were and returns what to tell. name is this computer's name on the
// phone.
func (a *trayAlerts) check(connected bool, p *phoneStatus, name string, now time.Time) []trayAlert {
	var out []trayAlert
	first := !a.seen
	a.seen = true
	if !connected {
		if a.downSince.IsZero() {
			a.downSince = now
		}
		// A short break (the tunnel reopening, the phone's network moving) is not worth a word.
		if !first && a.wasUp && !a.toldDown && now.Sub(a.downSince) >= 20*time.Second {
			a.toldDown = true
			out = append(out, trayAlert{"Telefonla bağlantı yok", "Telefon paylaşımı durdurdu ya da uzaklaştı. Paylaşım yeniden başlayınca bu bilgisayar kendiliğinden bağlanır.", true})
		}
		return out
	}
	if a.toldDown {
		out = append(out, trayAlert{"Telefona yeniden bağlandı", "İnternet yeniden telefondan geliyor.", false})
	}
	a.downSince, a.toldDown, a.wasUp = time.Time{}, false, true
	if p == nil {
		return out
	}
	if p.Battery >= 0 && !p.Charging && p.Battery <= 15 {
		if !a.toldBattery {
			a.toldBattery = true
			out = append(out, trayAlert{fmt.Sprintf("Telefonun pili %%%d", p.Battery), "Paylaşım sürsün istiyorsan telefonu şarja tak.", true})
		}
	} else if p.Charging || p.Battery > 20 {
		a.toldBattery = false
	}
	if p.Hot && !a.toldHot {
		out = append(out, trayAlert{"Telefon ısındı", "Soğuyana kadar hız 10 Mbit/s ile sınırlı.", true})
	}
	a.toldHot = p.Hot
	day := now.Format("2006-01-02")
	if p.LimitMB > 0 && p.Today >= p.LimitMB<<20*9/10 && a.toldLimit != day {
		a.toldLimit = day
		out = append(out, trayAlert{"Günlük veri sınırı dolmak üzere", fmt.Sprintf("Bugün %s kullanıldı; sınır dolunca telefon paylaşımı durdurur.", bytesText(p.Today)), true})
	}
	if pl := p.Plan; pl != nil && pl.LimitMB > 0 {
		level := 0
		switch {
		case pl.Used >= pl.LimitMB<<20:
			level = 100
		case pl.Used >= pl.LimitMB<<20*9/10:
			level = 90
		}
		if level > a.toldPlan {
			title := "Aylık paketin %90'ı kullanıldı"
			if level == 100 {
				title = "Aylık paket bitti"
			}
			out = append(out, trayAlert{title, fmt.Sprintf("Bu dönem %s kullanıldı; paket %d gün sonra yenilenir.", bytesText(pl.Used), pl.DaysLeft), true})
		}
		a.toldPlan = level
	}
	paused := ""
	for _, d := range p.Devices {
		if d.Name == name && d.Paused != "none" {
			paused = d.Paused
		}
	}
	if paused != "" && paused != a.toldPaused {
		why := map[string]string{
			"manual": "Telefondan duraklatıldı.",
			"limit":  "Bu bilgisayarın günlük sınırı doldu; gece yarısı devam eder.",
			"quiet":  "Şu an bu bilgisayar için internetin kapalı olduğu saatler.",
		}[paused]
		out = append(out, trayAlert{"Bu bilgisayarın interneti duraklatıldı", why, true})
	}
	a.toldPaused = paused
	return out
}

func bytesText(n int64) string {
	switch {
	case n >= 1<<30:
		return strings.Replace(fmt.Sprintf("%.1f GB", float64(n)/(1<<30)), ".", ",", 1)
	case n >= 1<<20:
		return fmt.Sprintf("%d MB", n>>20)
	default:
		return fmt.Sprintf("%d KB", n>>10)
	}
}

// installTrayAutostart starts the tray icon whenever someone signs in (needs administrator rights).
func installTrayAutostart() error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(runValue, `"`+installedExe()+`" tray`)
}

// removeTray takes the icon away: the autostart, and the icon of whoever runs it in this session.
func removeTray() {
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, runKey, registry.SET_VALUE); err == nil {
		_ = k.DeleteValue(runValue)
		k.Close()
	}
	class, _ := windows.UTF16PtrFromString(trayClass)
	for i := 0; i < 5; i++ {
		h, _, _ := findWindow.Call(uintptr(unsafe.Pointer(class)), 0)
		if h == 0 {
			return
		}
		postMessage.Call(h, wmClose, 0, 0)
		time.Sleep(300 * time.Millisecond)
	}
}

// startTray starts the icon if it does not run yet; the window does so, as it runs without administrator rights
// (an icon started with them would open the window with them too).
func startTray() {
	if isElevated() {
		return
	}
	name, _ := windows.UTF16PtrFromString(trayMutex)
	if h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, name); err == nil {
		windows.CloseHandle(h)
		return
	}
	if _, err := os.Stat(installedExe()); err == nil {
		_ = exec.Command(installedExe(), "tray").Start()
	}
}

// setMetered marks a Wi-Fi network as metered ("tarifeli bağlantı"): Windows Update, OneDrive and the Store hold
// their big downloads back on it by themselves. The user can switch it back in Windows' settings; it is set again
// only when the network is saved anew. Needs administrator rights.
func setMetered(ssid string) {
	if ssid == "" {
		return
	}
	if err := run("netsh.exe", "wlan", "set", "profileparameter", "name="+ssid, "cost=Fixed"); err != nil {
		logf("tarifeli bağlantı ayarlanamadı (%s): %v", ssid, err)
	}
}
