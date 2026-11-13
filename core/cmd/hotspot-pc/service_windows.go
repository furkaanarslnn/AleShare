package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// The official WireGuard for Windows names the service of a tunnel after its config file.
const serviceName = "WireGuardTunnel$" + tunnelName

func withService(fn func(*mgr.Service) error) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("servis yöneticisine bağlanılamadı (yönetici izni gerekir): %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("bağlantı servisi yok (%s): %w", serviceName, err)
	}
	defer s.Close()
	return fn(s)
}

func tunnelRunning() (bool, error) {
	var running bool
	err := withService(func(s *mgr.Service) error {
		st, err := s.Query()
		running = st.State == svc.Running || st.State == svc.StartPending
		return err
	})
	return running, err
}

func startTunnel() error {
	return withService(func(s *mgr.Service) error {
		if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
			return err
		}
		return nil
	})
}

func stopTunnel() error {
	return withService(func(s *mgr.Service) error {
		if _, err := s.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return err
		}
		return nil
	})
}

func isElevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

// relaunchElevated starts this program again with administrator rights; Windows
// shows its consent prompt. The new instance gets -elevated so that it keeps
// its window open at the end. The flag goes before the command's own arguments:
// after a word ("autojoin on") it would be taken for a second word, not a flag.
func relaunchElevated(command string, args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	all := append([]string{command, "-elevated"}, args...)
	quoted := make([]string, len(all))
	for i, a := range all {
		quoted[i] = syscall.EscapeArg(a)
	}
	err = windows.ShellExecute(0, windows.StringToUTF16Ptr("runas"), windows.StringToUTF16Ptr(exe),
		windows.StringToUTF16Ptr(strings.Join(quoted, " ")), windows.StringToUTF16Ptr(cwd), windows.SW_NORMAL)
	relaunched = err == nil
	return err
}
