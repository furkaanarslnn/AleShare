package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// The watcher service keeps the tunnel in step with the network: it runs what "reconcile" does once a minute, and
// a few seconds after a Wi-Fi adapter connects or disconnects. Up to 1.0.20 a scheduled task did this as SYSTEM;
// but while energy saver is on (a laptop unplugged and low, just when a phone's internet is wanted) Windows
// triggers only tasks that run as the signed-in user, and holds the others back until it ends (Microsoft:
// ITaskSettings remarks, "Energy saver" guidelines). Seen here: at 8 % the task did not run for 12 minutes. A
// service is not held back.

const (
	watchService   = "HotspotWatch"
	watchEvery     = time.Minute
	wlanLookEvery  = 3 * time.Second
	wlanSettle     = 2 * time.Second  // after a Wi-Fi change, before looking: Windows is still connecting
	reconcileLimit = 3 * time.Minute  // a run that takes longer is stuck: the service starts again
	stopWait       = 15 * time.Second // on Stop, how long a run under way may still take
)

// lockDir holds the files that keep runs of reconcile apart and say that a setup is under way: the private folder,
// which only SYSTEM and the administrators can write to. (Named objects, as these were, could be made first by any
// account of the computer, which then held the watcher still or hid a setup from it.) A variable for the tests.
var lockDir = privateDir

// reconcileWait is how long a run waits for another to finish; well within reconcileLimit, so that a run that only
// waited is not taken for stuck.
const reconcileWait = time.Minute

// lockFile opens name in lockDir and locks it, trying until wait is over (0: once). It returns the unlock, or nil
// when the file cannot be opened or stays locked.
func lockFile(name string, wait time.Duration) func() {
	f, err := os.OpenFile(filepath.Join(lockDir(), name), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil
	}
	h := windows.Handle(f.Fd())
	deadline := time.Now().Add(wait)
	for {
		err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{})
		if err == nil {
			var once sync.Once
			return func() {
				once.Do(func() {
					_ = windows.UnlockFileEx(h, 0, 1, 0, &windows.Overlapped{})
					f.Close()
				})
			}
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// What the Services list of Windows shows. The service's own name (watchService) keeps the program's old name, as
// do its folder and its tunnel: an installation is found again by them.
const (
	watchDisplayName = "AleShare"
	watchDescription = "Telefonun ağı görününce AleShare bağlantısını açar, ağdan çıkınca kapatır."
)

// holdSetup says, until resume is called or this process ends, that a setup is under way: the watcher service of
// an installation that is being paired anew then leaves the tunnel alone (see cmdReconcile). The mark is a lock on
// a file, which Windows lifts when the process ends: a setup whose window was closed half-way leaves none behind.
func holdSetup() (resume func()) {
	if unlock := lockFile("setup.lock", 0); unlock != nil {
		return unlock
	}
	return func() {}
}

// setupRunning reports whether a setup is under way in some process.
func setupRunning() bool {
	unlock := lockFile("setup.lock", 0)
	if unlock == nil {
		_, err := os.Stat(filepath.Join(lockDir(), "setup.lock"))
		return err == nil // there, and locked by a setup
	}
	unlock()
	return false
}

// inService says this process is the watcher service (an upgrade it runs itself is taken up by a restart).
var inService bool

func cmdService() error {
	inService = true
	return svc.Run(watchService, watcher{})
}

type watcher struct{}

func (watcher) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	started := modTime(installedExe())
	look := time.NewTicker(wlanLookEvery)
	defer look.Stop()
	wifi := wlanStates()
	next := time.Now() // the first run at once
	var (
		done    chan struct{}
		runEnds time.Time
		changed bool // the Wi-Fi changed while a run was under way, which may have looked before the change
	)
	for {
		now := time.Now()
		if done == nil && !now.Before(next) {
			done, runEnds = make(chan struct{}), now.Add(reconcileLimit)
			go func(done chan struct{}) {
				defer close(done)
				defer func() {
					if r := recover(); r != nil {
						logf("izleme: beklenmeyen hata: %v", r)
					}
				}()
				_ = cmdReconcile()
			}(done)
		}
		select {
		case r := <-requests:
			switch r.Cmd {
			case svc.Interrogate:
				status <- r.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: uint32(stopWait / time.Millisecond)}
				// A run under way may be installing a release: it gets a moment to finish rather than be cut off
				// half-way (see installSelf, which never leaves the program missing either way).
				if done != nil {
					select {
					case <-done:
					case <-time.After(stopWait):
					}
				}
				return false, 0
			}
		case <-done:
			done, next = nil, time.Now().Add(watchEvery)
			if changed {
				changed, next = false, time.Now().Add(wlanSettle)
			}
			// A newer release replaced the program (taken from the phone by this very run, or installed by hand):
			// stopping with an error makes Windows start the service again, from the new file.
			if t := modTime(installedExe()); !t.IsZero() && !t.Equal(started) {
				logf("yeni sürüm kuruldu: izleme hizmeti yeniden başlıyor")
				return true, 1
			}
		case <-look.C:
			if s := wlanStates(); s != wifi {
				wifi = s
				if done != nil {
					changed = true // the run's end would push the next look a minute away
				} else if soon := time.Now().Add(wlanSettle); soon.Before(next) {
					next = soon
				}
			}
			if done != nil && time.Now().After(runEnds) {
				logf("izleme: bir çalışma %v içinde bitmedi, hizmet yeniden başlıyor", reconcileLimit)
				return true, 2
			}
		}
	}
}

// lockReconcile keeps two runs of reconcile from overlapping (the service, and someone running it by hand); it
// returns the unlock. Without the lock (not installed yet, or another run holds it past reconcileWait) the run
// goes on regardless.
func lockReconcile() func() {
	if unlock := lockFile("reconcile.lock", reconcileWait); unlock != nil {
		return unlock
	}
	return func() {}
}

// wlanStates sums up the Wi-Fi adapters' connection states; it changes when one connects or disconnects. It asks
// the WLAN service directly: cheap enough for every few seconds, unlike netsh.
func wlanStates() string {
	var b strings.Builder
	_ = wlanInterfaces(func(_ windows.Handle, list []wlanInterfaceInfo) {
		for _, i := range list {
			fmt.Fprintf(&b, "%v=%d;", i.guid, i.state)
		}
	})
	return b.String()
}

// wlanInterfaces hands fn the Wi-Fi adapters, with an open WLAN handle.
func wlanInterfaces(fn func(windows.Handle, []wlanInterfaceInfo)) error {
	var negotiated uint32
	var handle windows.Handle
	if r, _, _ := wlanOpen.Call(wlanClientAPI, 0, uintptr(unsafe.Pointer(&negotiated)), uintptr(unsafe.Pointer(&handle))); r != 0 {
		return fmt.Errorf("WlanOpenHandle: %w", syscall.Errno(r))
	}
	defer wlanClose.Call(uintptr(handle), 0)
	var list unsafe.Pointer
	if r, _, _ := wlanEnum.Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&list))); r != 0 {
		return fmt.Errorf("WlanEnumInterfaces: %w", syscall.Errno(r))
	}
	defer wlanFreeMem.Call(uintptr(list))
	// WLAN_INTERFACE_INFO_LIST: a count and an index (4 bytes each), then the entries.
	count := *(*uint32)(list)
	fn(handle, unsafe.Slice((*wlanInterfaceInfo)(unsafe.Add(list, 8)), count))
	return nil
}

// ---- installing it (with administrator rights) ----

// installWatchService makes the watcher service run from the installed program whenever Windows runs, restarted
// when it stops with an error, and removes the scheduled task that did its work before.
func installWatchService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(watchService)
	if err == nil {
		cfg, err := s.Config()
		if err == nil {
			cfg.BinaryPathName = `"` + installedExe() + `" service`
			cfg.StartType = mgr.StartAutomatic
			cfg.DisplayName, cfg.Description = watchDisplayName, watchDescription
			err = s.UpdateConfig(cfg)
		}
		if err != nil {
			s.Close()
			return err
		}
	} else {
		s, err = m.CreateService(watchService, installedExe(), mgr.Config{
			StartType:   mgr.StartAutomatic,
			DisplayName: watchDisplayName,
			Description: watchDescription,
		}, "service")
		if err != nil {
			return err
		}
	}
	defer s.Close()
	restart := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 2 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}
	if err := s.SetRecoveryActions(restart, uint32((24 * time.Hour).Seconds())); err != nil {
		return err
	}
	if err := s.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
		return err
	}
	_ = command("schtasks.exe", "/Delete", "/TN", taskName, "/F").Run()
	return nil
}

// startWatchService starts the service unless it runs.
func startWatchService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(watchService)
	if err != nil {
		return err
	}
	defer s.Close()
	if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return err
	}
	return nil
}

// stopWatchService stops the service if there is one and waits a while for it (a run under way is dropped).
func stopWatchService() {
	m, err := mgr.Connect()
	if err != nil {
		return
	}
	defer m.Disconnect()
	s, err := m.OpenService(watchService)
	if err != nil {
		return
	}
	defer s.Close()
	st, err := s.Control(svc.Stop)
	for i := 0; err == nil && st.State != svc.Stopped && i < 40; i++ {
		time.Sleep(250 * time.Millisecond)
		st, err = s.Query()
	}
}

// removeWatchService stops the service and deletes it.
func removeWatchService() {
	stopWatchService()
	m, err := mgr.Connect()
	if err != nil {
		return
	}
	defer m.Disconnect()
	if s, err := m.OpenService(watchService); err == nil {
		_ = s.Delete()
		s.Close()
	}
}

// watchServiceState says how the service is, for "status": running, stopped, or missing.
func watchServiceState() string {
	m, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return "bilinmiyor"
	}
	defer windows.CloseServiceHandle(m)
	s, err := windows.OpenService(m, windows.StringToUTF16Ptr(watchService), windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return "yok"
	}
	defer windows.CloseServiceHandle(s)
	var st windows.SERVICE_STATUS
	if windows.QueryServiceStatus(s, &st) != nil {
		return "bilinmiyor"
	}
	if st.CurrentState == windows.SERVICE_RUNNING {
		return "çalışıyor"
	}
	return "durdu"
}
