package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hotspot/core/internal/release"
)

// The user's own folder: the window and the icon run with the user's rights and cannot write the program's.
func onlineDir() string { return filepath.Join(os.Getenv("LOCALAPPDATA"), "Hotspot") }

func onlinePath() string { return filepath.Join(onlineDir(), "online.json") }

func loadOnline() onlineState {
	var s onlineState
	if data, err := os.ReadFile(onlinePath()); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

func saveOnline(s onlineState) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(onlineDir(), 0o755)
	part := onlinePath() + ".part"
	if os.WriteFile(part, data, 0o644) == nil {
		_ = os.Rename(part, onlinePath())
	}
}

var (
	onlineClient = &http.Client{Timeout: 20 * time.Second}
	onlineFiles  = &http.Client{Timeout: 15 * time.Minute}
	onlineLook   sync.Mutex // one look at a time in this process
)

func onlineGet(c *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "AleShare/"+version)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GitHub %s dedi", resp.Status)
	}
	return resp, nil
}

// checkOnline asks GitHub for the newest release when it is time to (or at once with force), and returns what is
// known afterwards. A look that fails changes nothing: without internet there is simply no news.
func checkOnline(force bool) onlineState {
	onlineLook.Lock()
	defer onlineLook.Unlock()
	s := loadOnline()
	now := time.Now()
	if !force && !s.due(now) {
		return s
	}
	s.Tried = now
	if resp, err := onlineGet(onlineClient, manifestURL); err == nil {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if rel, err := parseManifest(data); err == nil {
			told := loadOnline().Told // the icon may have told meanwhile
			s.Checked, s.Release, s.Told = now, &rel, told
		}
	}
	saveOnline(s)
	return s
}

// fetchOnlineProgram downloads the release's Windows program into the user's folder and returns its path, once it
// is the file the release names and carries this project's signature for exactly that release.
func fetchOnlineProgram(rel onlineRelease) (string, error) {
	dir := filepath.Join(onlineDir(), "update")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "AleShare.exe")
	resp, err := onlineGet(onlineFiles, rel.PC.URL)
	if err != nil {
		return "", fmt.Errorf("indirilemedi: %w", err)
	}
	defer resp.Body.Close()
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, sum), io.LimitReader(resp.Body, rel.PC.Size+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return "", fmt.Errorf("indirilemedi: %w", err)
	}
	if n != rel.PC.Size || !strings.EqualFold(hex.EncodeToString(sum.Sum(nil)), rel.PC.SHA256) {
		os.Remove(path)
		return "", errors.New("indirilen dosya bozuk; yeniden dene")
	}
	if _, err := verifiedRelease(path, rel.Code); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// verifiedRelease reads the program at path and returns its bytes when it carries this project's release
// signature; want, when not 0, is the release it must be.
func verifiedRelease(path string, want int) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	info, err := release.Verify(data)
	if err != nil {
		return nil, errors.New("dosya AleShare'in imzasını taşımıyor; kurulmadı")
	}
	if want != 0 && info.Code != want {
		return nil, fmt.Errorf("dosya %s sürümü değil; kurulmadı", release.Name(want))
	}
	return data, nil
}

// upgradeFromFile installs the program in the file at path over the installed one. It runs with administrator
// rights while the file lies where the user (and whatever runs as the user) can write, so the file is read once,
// its signature is checked on what was read, and exactly those bytes are installed.
func upgradeFromFile(path string) error {
	data, err := verifiedRelease(path, 0)
	if err != nil {
		return err
	}
	info, _ := release.Read(data)
	if own := versionCode(version); own > 0 && info.Code <= own {
		return fmt.Errorf("%s bu bilgisayardakinden yeni değil", release.Name(info.Code))
	}
	own := filepath.Join(dataDir(), "hotspot-pc.online")
	_ = os.Remove(own)
	f, err := os.OpenFile(own, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	defer os.Remove(own)
	if err != nil {
		return err
	}
	if err := upgradeFrom(own, info.Name); err != nil {
		return err
	}
	logf("GitHub'dan %s geldi ve kuruldu (imza doğru)", info.Name)
	return nil
}

// cmdCheck asks GitHub now and says what the newest release is.
func cmdCheck() error {
	s := checkOnline(true)
	if s.Release == nil || time.Since(s.Checked) > time.Minute {
		return errors.New("GitHub'a ulaşılamadı ya da henüz yayımlanmış bir sürüm yok")
	}
	fmt.Printf("En yeni sürüm: %s   Bu program: %s\n", s.Release.Name, version)
	if s.offer(versionCode(version)) != nil {
		fmt.Println("Yeni sürüm var: AleShare penceresini açıp Güncelle'ye tıkla.")
	} else {
		fmt.Println("AleShare güncel.")
	}
	return nil
}
