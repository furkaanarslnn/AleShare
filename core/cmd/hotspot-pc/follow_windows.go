package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hotspot/core/internal/vpnclient"
	"hotspot/core/internal/wifiinfo"
)

// askTimeout bounds one question to the phone; it answers within milliseconds when it is there.
const askTimeout = 2 * time.Second

// askPhone asks the phone about its Wi-Fi network, proving with the tunnel's keys that this computer is paired.
// While the tunnel is up the question goes through it (WireGuard's firewall lets nothing else out), otherwise
// to the phone on its Wi-Fi network.
func askPhone(set settings, viaTunnel bool) (wifiinfo.Info, error) {
	data, err := os.ReadFile(confPath())
	if err != nil {
		return wifiinfo.Info{}, err
	}
	cfg, err := vpnclient.Parse(string(data))
	if err != nil {
		return wifiinfo.Info{}, err
	}
	host := set.Phone
	if viaTunnel {
		host = tunnelGateway
	}
	addr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(host, strconv.Itoa(wifiinfo.Port)))
	if err != nil {
		return wifiinfo.Info{}, err
	}
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return wifiinfo.Info{}, err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), askTimeout)
	defer cancel()
	return wifiinfo.Ask(ctx, conn, addr, cfg.PrivateKey, cfg.PublicKey)
}

// followPhone asks the phone about its network and follows a change of name or password, and reports whether it
// answered. A phone that does not answer (its app is older, or not running) changes nothing.
func followPhone(set settings, viaTunnel bool) (settings, bool) {
	info, err := askPhone(set, viaTunnel)
	if err != nil {
		return set, false
	}
	set = applyFollow(set, info)
	syncReleases(set, info, viaTunnel) // the phone is there: newer releases move one way or the other
	return set, true
}

// adoptNetwork recognizes the phone's network under a name this computer missed: the user changed it while the
// computer was off, then joined the new network on this computer by hand. Only the paired phone can answer the
// question (and it does not name this computer), so a stranger's network that happens to look alike is not taken
// for it.
func adoptNetwork(set settings, ssids []string) (settings, bool) {
	for _, s := range ssids {
		if !strings.HasPrefix(s, directPrefix) || knownNetwork(set, s) {
			continue
		}
		info, err := askPhone(set, false)
		if err != nil || info.Current.SSID != s {
			continue
		}
		logf("telefonun ağı yeni adıyla tanındı: %s", s)
		return applyFollow(set, info), true
	}
	return set, false
}

// applyFollow carries out what planFollow decides and returns the settings in force afterwards.
func applyFollow(set settings, info wifiinfo.Info) settings {
	applied := loadNextWifi()
	next, nextApplied, plan := planFollow(set, applied, info)
	if len(plan.add) > 0 {
		// The new profile behaves like the one the user made: joined by Windows by itself, or only on request.
		mode := exportedMode(set.Profile)
		for _, n := range plan.add {
			if err := addWifiProfile(n, mode); err != nil {
				logf("Wi-Fi profili yazılamadı (%s): %v", n.SSID, err)
				return set // tried again on the next run
			}
			logf("telefonun ağı kaydedildi: %s", n.SSID)
		}
	}
	for _, name := range plan.remove {
		if err := deleteWifiProfile(name); err != nil {
			logf("telefonun eski ağı silinemedi (%s): %v", name, err)
		} else {
			logf("telefonun eski ağı unutuldu: %s", name)
		}
	}
	if next != set {
		if err := saveSettings(next); err != nil {
			logf("ayarlar yazılamadı: %v", err)
			return set
		}
		if next.Next != "" {
			logf("telefon ağını değiştiriyor: şimdiki %q, sonraki %q", next.Profile, next.Next)
		} else {
			logf("telefonun ağı: %q", next.Profile)
		}
	}
	if (applied == nil) != (nextApplied == nil) || (applied != nil && *applied != *nextApplied) {
		saveNextWifi(nextApplied)
	}
	return next
}

// addWifiProfile makes Windows know a network of the phone with its password. The file that carries the password
// to netsh lives for a moment in the private folder, which only the system and administrators can read.
func addWifiProfile(n wifiinfo.Network, mode string) error {
	path := filepath.Join(privateDir(), "wifi-profile.xml")
	if err := os.WriteFile(path, []byte(wlanProfileXML(n, mode)), 0o600); err != nil {
		return err
	}
	defer os.Remove(path)
	if err := run("netsh.exe", "wlan", "add", "profile", "filename="+path, "user=all"); err != nil {
		return err
	}
	setMetered(n.SSID)
	return nil
}

// deleteWifiProfile makes Windows forget a network of the phone, on every adapter, and checks that it did:
// netsh reports success even when it found nothing to delete.
func deleteWifiProfile(name string) error {
	out, _ := command("netsh.exe", "wlan", "delete", "profile", "name="+name, "interface=*").CombinedOutput()
	list, err := command("netsh.exe", "wlan", "show", "profiles").Output()
	if err != nil {
		return err
	}
	if profileListed(string(list), name) {
		return fmt.Errorf("Windows listede tutuyor: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// exportedMode is the connection mode of a Windows Wi-Fi profile ("auto" or "manual"; "" when unknown). The export
// keeps the password encrypted.
func exportedMode(profile string) string {
	if profile == "" {
		return ""
	}
	dir, err := os.MkdirTemp(privateDir(), "export")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(dir)
	if command("netsh.exe", "wlan", "export", "profile", "name="+profile, "folder="+dir).Run() != nil {
		return ""
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.xml"))
	for _, f := range files {
		if data, err := os.ReadFile(f); err == nil {
			if m := profileMode(string(data)); m != "" {
				return m
			}
		}
	}
	return ""
}
