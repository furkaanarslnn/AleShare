package main

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	wlanapi       = windows.NewLazySystemDLL("wlanapi.dll")
	wlanOpen      = wlanapi.NewProc("WlanOpenHandle")
	wlanClose     = wlanapi.NewProc("WlanCloseHandle")
	wlanEnum      = wlanapi.NewProc("WlanEnumInterfaces")
	wlanScan      = wlanapi.NewProc("WlanScan")
	wlanFreeMem   = wlanapi.NewProc("WlanFreeMemory")
	kernel32      = windows.NewLazySystemDLL("kernel32.dll")
	consoleProcs  = kernel32.NewProc("GetConsoleProcessList")
	wlanClientAPI = uintptr(2) // Windows Vista and later
)

// wlanInterfaceInfo is WLAN_INTERFACE_INFO.
type wlanInterfaceInfo struct {
	guid        windows.GUID
	description [256]uint16
	state       uint32
}

// scanWifi asks every Wi-Fi adapter for a fresh list of networks. Windows scans on its own only
// every few minutes while it is connected, which would leave the phone's new network unseen.
func scanWifi() error {
	return wlanInterfaces(func(handle windows.Handle, list []wlanInterfaceInfo) {
		for i := range list {
			wlanScan.Call(uintptr(handle), uintptr(unsafe.Pointer(&list[i].guid)), 0, 0, 0)
		}
	})
}

// ownsConsole reports whether this program is the only one using its console, which is the case when it
// was started by double-clicking: the window then closes the moment the program ends.
func ownsConsole() bool {
	var pids [2]uint32
	n, _, _ := consoleProcs.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return n == 1
}

// relaunched is set once the work has moved on to an elevated copy of this program, which has its own window.
var relaunched bool

// pauseIfOwnConsole keeps a double-clicked window open until the user has read it.
func pauseIfOwnConsole() {
	if ownsConsole() && !isElevatedRelaunch() && !relaunched {
		waitEnter("\nKapatmak için Enter'a bas...")
	}
}

func isElevatedRelaunch() bool {
	for _, a := range os.Args[1:] {
		if a == "-elevated" {
			return true
		}
	}
	return false
}
