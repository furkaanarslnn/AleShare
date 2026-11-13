package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"hotspot/core/internal/wifiinfo"
)

const (
	tunnelName = "hotspot"
	logLimit   = 100 << 10
)

// settings are written at setup and read by the watcher service.
type settings struct {
	Phone   string `json:"phone"`   // the phone's address on its own network
	Profile string `json:"profile"` // the network's name on this computer
	// Next is the name the phone announced for its network while the user changes it; the computer is at home
	// on it as on Profile, and it becomes Profile once the phone has moved.
	Next string `json:"next,omitempty"`
	// AutoJoin lets the watcher service connect to the phone's network by itself when it is in range.
	// Off unless the user asked for it: it moves this computer off whatever network it is on.
	AutoJoin bool `json:"auto_join"`
	// Version is the release of the installed program (empty for installations older than 1.0.11).
	Version string `json:"version,omitempty"`
	// Metered says the phone's network was marked metered once (see setMetered); after that it is the user's.
	Metered bool `json:"metered,omitempty"`
}

// version is this program's release, set by the build (-X main.version=1.1).
var version = "geliştirme"

func dataDir() string { return filepath.Join(os.Getenv("ProgramData"), "Hotspot") }

func privateDir() string { return filepath.Join(dataDir(), "private") }

func confPath() string { return filepath.Join(privateDir(), tunnelName+".conf") }

func settingsPath() string { return filepath.Join(dataDir(), "settings.json") }

func installedExe() string { return filepath.Join(dataDir(), "hotspot-pc.exe") }

func loadSettings() (settings, error) {
	var s settings
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		return s, fmt.Errorf("kurulu değil (%s okunamadı): %w", settingsPath(), err)
	}
	return s, json.Unmarshal(data, &s)
}

func saveSettings(s settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath(), data, 0o644)
}

// nextWifiPath holds the network that follows, password included, as a Windows profile was made for it. It is
// in the private folder, like the tunnel's key.
func nextWifiPath() string { return filepath.Join(privateDir(), "next-wifi.json") }

func loadNextWifi() *wifiinfo.Network {
	var n wifiinfo.Network
	data, err := os.ReadFile(nextWifiPath())
	if err != nil || json.Unmarshal(data, &n) != nil || n.SSID == "" {
		return nil
	}
	return &n
}

func saveNextWifi(n *wifiinfo.Network) {
	if n == nil {
		_ = os.Remove(nextWifiPath())
		return
	}
	if data, err := json.Marshal(n); err == nil {
		_ = os.WriteFile(nextWifiPath(), data, 0o600)
	}
}

func healthPath() string { return filepath.Join(dataDir(), "health.json") }

func loadHealth() health {
	var h health
	if data, err := os.ReadFile(healthPath()); err == nil {
		_ = json.Unmarshal(data, &h)
	}
	return h
}

func saveHealth(h health) {
	if data, err := json.Marshal(h); err == nil {
		_ = os.WriteFile(healthPath(), data, 0o644)
	}
}

// logf records state changes only: never an address someone visited.
func logf(format string, args ...any) {
	path := filepath.Join(dataDir(), "hotspot.log")
	if st, err := os.Stat(path); err == nil && st.Size() > logLimit {
		os.Remove(path)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s  %s\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
}
