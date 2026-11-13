package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"hotspot/core/internal/release"
	"hotspot/core/internal/wifiinfo"
)

func releasesPath() string { return filepath.Join(dataDir(), "releases.json") }

// keptAPK is the newest APK this computer has, to give a phone that is behind.
func keptAPK() string { return filepath.Join(dataDir(), "Hotspot.apk") }

func loadReleases() releaseState {
	var s releaseState
	if data, err := os.ReadFile(releasesPath()); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

func saveReleases(s releaseState) {
	if data, err := json.MarshalIndent(s, "", "  "); err == nil {
		_ = os.WriteFile(releasesPath(), data, 0o644)
	}
}

// keepSiblingAPK keeps the APK that lies beside this program (a release's two files are downloaded into one
// folder), when it is a newer release than the one kept. The published APK is called AleShare.apk whatever its
// release, so the release is read out of the file; older builds wrote it into the name.
func keepSiblingAPK(program string) {
	s := loadReleases()
	dir := filepath.Dir(program)
	entries, _ := os.ReadDir(dir)
	best, bestPath := s.APK, ""
	for _, e := range entries {
		n := apkRelease(e.Name())
		if publishedAPK.MatchString(e.Name()) {
			n = release.APKCode(filepath.Join(dir, e.Name()))
		}
		if n > best {
			best, bestPath = n, filepath.Join(dir, e.Name())
		}
	}
	if bestPath == "" {
		return
	}
	if err := copyFile(bestPath, keptAPK()); err != nil {
		logf("APK saklanamadı: %v", err)
		return
	}
	s.APK = best
	saveReleases(s)
	logf("APK %s saklandı (telefonlara verilmek üzere)", release.Name(best))
}

// nameOf is what the phone calls a release it told of; a phone that gave no name leaves it to the number.
func nameOf(a *wifiinfo.App) string {
	if a == nil {
		return ""
	}
	if a.Name != "" {
		return a.Name
	}
	return release.Name(a.Code)
}

// syncReleases notes what the phone said it has (info) and, while the tunnel is up, moves releases between this
// computer and the phone through it. It runs in the watcher service, as SYSTEM.
func syncReleases(set settings, info wifiinfo.Info, viaTunnel bool) {
	if info.App == nil || info.App.Token == "" {
		return // an older phone app: it neither hands out nor takes releases this way
	}
	savePhoneAccess(info.App.Token) // for the window (see phone_windows.go)
	s := loadReleases()
	phonePC := 0
	if info.PC != nil {
		phonePC = info.PC.Code
	}
	if s.PhoneApp != info.App.Code || s.PhonePC != phonePC || s.PhoneAppName != nameOf(info.App) {
		s.PhoneApp, s.PhonePC, s.PhoneAppName = info.App.Code, phonePC, nameOf(info.App)
		saveReleases(s)
	}
	if !viaTunnel {
		// The tunnel is about to open and the user waits for the internet: files that take minutes do not go
		// first. They move on the next look, through the tunnel.
		return
	}
	url := func(file string) string {
		return "http://" + net.JoinHostPort(tunnelGateway, strconv.Itoa(updatePort)) + "/" + file + "?t=" + info.App.Token
	}
	own := versionCode(version)
	plan := planSync(own, s.APK, info.App.Code, phonePC)
	now := time.Now()
	// The try is written down before it starts: one that is cut short (the service restarted, the computer went
	// to sleep) then waits its time like one the phone turned away, instead of being tried again every minute.
	try := func(what string, code int) bool {
		if !s.due(what, code, now) {
			return false
		}
		saveReleases(s)
		return true
	}
	// One transfer a look: each may take minutes, and a look that takes too long is taken for stuck (see
	// reconcileLimit). The others follow on the next looks, a minute apart.
	switch {
	case plan.takeProgram && try("take-pc", phonePC):
		if err := takeProgram(url("hotspot-pc.exe"), own); err != nil {
			logf("telefondaki program (%s) alınamadı: %v", nameOf(info.PC), err)
		}
	case plan.bringProgram && try("bring-pc", own):
		if data, err := os.ReadFile(installedExe()); err != nil {
			logf("program okunamadı: %v", err)
		} else if _, err := release.Verify(data); err != nil {
			// A build that is not a release (or a program older than release signing) is not handed around.
		} else if err := post(url("hotspot-pc.exe"), data); err != nil {
			logf("program telefona verilemedi: %v", err)
		} else {
			logf("program %s telefona verildi", version)
		}
	case plan.takeAPK && try("take-apk", info.App.Code):
		if err := download(url("hotspot.apk"), keptAPK(), 200<<20); err != nil {
			logf("telefondaki APK (%s) alınamadı: %v", nameOf(info.App), err)
		} else {
			s.APK = info.App.Code
			saveReleases(s)
			logf("telefondaki APK %s saklandı", nameOf(info.App))
		}
	case plan.bringAPK && try("bring-apk", s.APK):
		if data, err := os.ReadFile(keptAPK()); err != nil {
			logf("APK okunamadı: %v", err)
		} else if err := post(url("hotspot.apk")+"&code="+strconv.Itoa(s.APK), data); err != nil {
			logf("APK telefona verilemedi: %v", err)
		} else {
			logf("APK %s telefona verildi; telefonda kurulum onayı bekleniyor", release.Name(s.APK))
		}
	}
}

// updatePort is the phone's store (wgcore.UpdatePort).
const updatePort = 51823

// transfers moves one file to or from the phone, in less time than a look of the watcher may take (reconcileLimit).
var transfers = &http.Client{Timeout: 150 * time.Second}

// takeProgram downloads the phone's Windows program and installs it in place of this one, if it carries this
// project's release signature and is newer than own. The watcher's next look is the new program's.
func takeProgram(url string, own int) error {
	path := filepath.Join(dataDir(), "hotspot-pc.download")
	defer os.Remove(path)
	if err := download(url, path, 64<<20); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	info, err := release.Verify(data)
	if err != nil {
		return err
	}
	if info.Code <= own {
		return fmt.Errorf("%s bu bilgisayardakinden yeni değil", release.Name(info.Code))
	}
	if err := upgradeFrom(path, info.Name); err != nil {
		return err
	}
	logf("telefondan %s geldi ve kuruldu (imza doğru)", info.Name)
	return nil
}

func download(url, path string, max int64) error {
	resp, err := transfers.Get(url)
	if err != nil {
		return bare(err) // without the address: it carries the phone's token, and this error is logged
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telefon %s dedi", resp.Status)
	}
	part := path + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, max+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > max {
		err = fmt.Errorf("%d bayttan büyük", max)
	}
	if err != nil {
		os.Remove(part)
		return err
	}
	return os.Rename(part, path)
}

func post(url string, data []byte) error {
	resp, err := transfers.Post(url, "application/octet-stream", bytes.NewReader(data))
	if err != nil {
		return bare(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("telefon %s dedi: %s", resp.Status, bytes.TrimSpace(msg))
	}
	return nil
}
