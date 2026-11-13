package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"hotspot/core/internal/pairing"
	"hotspot/core/internal/wifiinfo"
)

func defaultName() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "bilgisayar"
}

func waitEnter(prompt string) {
	fmt.Print(prompt)
	bufio.NewReader(os.Stdin).ReadString('\n')
}

func networks() ([]netEntry, error) {
	const script = `[Console]::OutputEncoding = [Text.Encoding]::UTF8; Get-NetIPConfiguration | ForEach-Object { [pscustomobject]@{ Alias = $_.InterfaceAlias; Profile = $_.NetProfile.Name; Gateway = (@($_.IPv4DefaultGateway)[0]).NextHop } } | ConvertTo-Json -Compress`
	out, err := command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return nil, fmt.Errorf("ağ bilgisi okunamadı: %w", err)
	}
	return parseNetworks(out)
}

// detectPhone finds the phone: on its own network it is this computer's default gateway.
func detectPhone(override string) (phone, profile string, err error) {
	nets, err := networks()
	if err != nil {
		return "", "", err
	}
	if override != "" {
		for _, n := range nets {
			if n.Gateway == override {
				return override, n.Profile, nil
			}
		}
		return override, "", nil
	}
	var candidates []netEntry
	for _, n := range nets {
		if n.Gateway != "" {
			candidates = append(candidates, n)
		}
	}
	for _, n := range candidates {
		if strings.HasPrefix(n.Profile, directPrefix) {
			return n.Gateway, n.Profile, nil
		}
	}
	if len(candidates) == 1 {
		return candidates[0].Gateway, candidates[0].Profile, nil
	}
	if len(candidates) == 0 {
		return "", "", errors.New("hiçbir ağa bağlı değilsin; önce telefonun Wi-Fi ağına bağlan")
	}
	var list []string
	for _, n := range candidates {
		list = append(list, fmt.Sprintf("%s (%q)", n.Gateway, n.Profile))
	}
	return "", "", fmt.Errorf("birden fazla ağ var: %s. Telefonun adresini -phone ile ver", strings.Join(list, ", "))
}

// directPrefix starts the name of a Wi-Fi Direct network, which is what the app opens.
const directPrefix = "DIRECT-"

// waitFor calls ready every two seconds until it returns true or the time is up.
func waitFor(limit time.Duration, ready func() bool) bool {
	deadline := time.Now().Add(limit)
	for {
		if ready() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Second)
	}
}

// waitForPairingWindow waits until the phone accepts pairing requests. It does so while the app is open on
// the screen with sharing on; a connection that is only tried and closed counts for nothing there.
func waitForPairingWindow(ui setupUI, phoneIP string) error {
	addr := net.JoinHostPort(phoneIP, strconv.Itoa(pairing.Port))
	announced := false
	ok := waitFor(5*time.Minute, func() bool {
		c, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			c.Close()
			return true
		}
		if !announced {
			ui.Step(stepPair, "Telefonda AleShare uygulamasını aç, paylaşımı başlat ve uygulamayı ekranda açık tut.")
			announced = true
		}
		return false
	})
	if !ok {
		return errors.New("telefon eşleşme isteği kabul etmiyor (uygulama açık ve paylaşım başlamış mı?)")
	}
	return nil
}

// runPairing pairs by comparing six digits, the way of a phone app older than QR pairing.
func runPairing(ui setupUI, phoneIP, name string) (*pairing.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), pairing.DefaultWindow)
	defer cancel()
	res, err := pairing.Pair(ctx, net.JoinHostPort(phoneIP, strconv.Itoa(pairing.Port)), name, ui.Number)
	if errors.Is(err, pairing.ErrDenied) {
		return nil, errors.New("telefonda reddedildi")
	}
	if err != nil {
		return nil, fmt.Errorf("%w\n(telefonda AleShare uygulaması ekranda açık ve paylaşım başlamış mı, aynı ağda mısın?)", err)
	}
	return res, nil
}

// cmdPair pairs and writes a configuration, nothing more: no installation.
func cmdPair(args []string) error {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	phone := fs.String("phone", "", "telefonun IP adresi (varsayılan: bağlı ağın ağ geçidi)")
	name := fs.String("name", defaultName(), "telefonda görünecek cihaz adı")
	out := fs.String("out", "hotspot.conf", "yapılandırmanın yazılacağı dosya")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ip, _, err := detectPhone(*phone)
	if err != nil {
		return err
	}
	res, err := runPairing(consoleUI{}, ip, *name)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, []byte(buildConf(res, ip)), 0o600); err != nil {
		return err
	}
	fmt.Printf("Eşleşti. Yapılandırma yazıldı: %s (özel anahtar içerir, kullandıktan sonra sil)\n", *out)
	return nil
}

func findWireGuard() string {
	for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramW6432"), os.Getenv("ProgramFiles(x86)")} {
		if base == "" {
			continue
		}
		p := filepath.Join(base, "WireGuard", "wireguard.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// installWireGuard installs the official WireGuard for Windows without asking: with winget where it is there, or
// else with the signed package from WireGuard's own download page. Either needs this computer's internet once.
func installWireGuard(ui setupUI) error {
	ui.Step(stepWireGuard, "WireGuard indirilip kuruluyor (bir kez internet gerekir)…")
	if _, err := exec.LookPath("winget.exe"); err == nil {
		cmd := command("winget.exe", "install", "--id", "WireGuard.WireGuard", "-e", "--silent",
			"--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity")
		cmd.Stdout, cmd.Stderr = ui.Out(), ui.Out()
		if cmd.Run() == nil && findWireGuard() != "" {
			return nil
		}
		ui.Step(stepWireGuard, "WireGuard indiriliyor…")
	}
	return installWireGuardMSI()
}

// installWireGuardMSI downloads the newest package for this computer from download.wireguard.com, makes sure
// Windows finds it signed by WireGuard LLC, and installs it quietly. The file lies in the private folder meanwhile:
// in the temporary folder, anything running as the user could swap it between the check and the installation,
// which runs with administrator rights.
func installWireGuardMSI() error {
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(wireGuardDownloads)
	if err != nil {
		return fmt.Errorf("WireGuard'ın sitesine ulaşılamadı (bu bilgisayarın interneti var mı?): %w", err)
	}
	listing, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if err != nil {
		return err
	}
	name, ok := newestMSI(string(listing), runtime.GOARCH)
	if !ok {
		return errors.New("WireGuard'ın sitesinde bu bilgisayara uygun paket bulunamadı")
	}
	path := filepath.Join(privateDir(), name)
	defer os.Remove(path)
	resp, err = client.Get(wireGuardDownloads + name)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s indirilemedi: %s", name, resp.Status)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, io.LimitReader(resp.Body, 200<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	// The same check Windows makes, done before running it: a valid signature of WireGuard LLC.
	check := `$s = Get-AuthenticodeSignature -LiteralPath '` + strings.ReplaceAll(path, "'", "''") + `'; ` +
		`if ($s.Status -eq 'Valid' -and $s.SignerCertificate.Subject -like 'CN=WireGuard LLC,*') { 'ok' }`
	if powershell(check) != "ok" {
		return fmt.Errorf("%s WireGuard LLC imzası taşımıyor, kurulmadı", name)
	}
	if err := run("msiexec.exe", "/i", path, "/qn", "/norestart", "DO_NOT_LAUNCH=1"); err != nil {
		return err
	}
	if findWireGuard() == "" {
		return errors.New("WireGuard kuruldu ama wireguard.exe bulunamadı")
	}
	return nil
}

// onPhoneNetwork reports the phone's address and network when this computer is on the network the app opened.
func onPhoneNetwork() (phone, profile string, ok bool) {
	nets, err := networks()
	if err != nil {
		return "", "", false
	}
	for _, n := range nets {
		if strings.HasPrefix(n.Profile, directPrefix) && n.Gateway != "" {
			return n.Gateway, n.Profile, true
		}
	}
	return "", "", false
}

// joinPhoneWifi brings this computer onto the phone's own network: at once if it is there, through the profile
// Windows keeps if it knows the network, or with the password the user types here once. Nothing else can carry
// the password over: until this computer is on that network it cannot reach the phone at all.
func joinPhoneWifi(ui setupUI) (phone, profile string, err error) {
	ui.Step(stepWifi, "Telefonun Wi-Fi ağı aranıyor. Telefonda AleShare'i aç ve Paylaşımı başlat'a dokun.")
	deadline := time.Now().Add(10 * time.Minute)
	retry := false
	failed := map[string]int{} // how often the profile Windows had for a network did not get this computer onto it
	for time.Now().Before(deadline) {
		if phone, profile, ok := onPhoneNetwork(); ok {
			return phone, profile, nil
		}
		_ = scanWifi()
		time.Sleep(4 * time.Second) // the scan answers asynchronously
		out, _ := command("netsh.exe", "wlan", "show", "networks").Output()
		found := phoneNetworks(parseVisibleSSIDs(string(out)))
		if len(found) == 0 {
			time.Sleep(3 * time.Second)
			continue
		}
		ssid := found[0]
		if len(found) > 1 {
			ssid = ui.Choose("Birden fazla Wi-Fi Direct ağı görünüyor (yazıcılar da böyle bir ağ açar). Telefonunki hangisi?", found)
		}
		profiles, _ := command("netsh.exe", "wlan", "show", "profiles").Output()
		added := false
		// A profile Windows kept from before may hold a password the phone no longer uses (the user changed it, or
		// the app was installed anew). After it failed twice it is written anew, like one that was never there.
		stale := failed[ssid] >= 2
		if !profileListed(string(profiles), ssid) || stale {
			password, ok := ui.Password(ssid, retry || stale)
			if !ok {
				return "", "", errCancelled
			}
			n := wifiinfo.Network{SSID: ssid, Password: password}
			if !n.Valid() {
				retry = true
				continue
			}
			if err := addWifiProfile(n, "auto"); err != nil {
				return "", "", fmt.Errorf("Wi-Fi profili yazılamadı: %w", err)
			}
			added = true
		}
		ui.Step(stepWifi, ssid+" ağına bağlanılıyor…")
		_ = run("netsh.exe", "wlan", "connect", "name="+ssid, "ssid="+ssid)
		if waitFor(30*time.Second, func() bool { _, _, ok := onPhoneNetwork(); return ok }) {
			continue // the top of the loop reports it
		}
		if added {
			_ = deleteWifiProfile(ssid) // most likely a wrong password: ask again
			retry = true
		} else {
			failed[ssid]++
			ui.Step(stepWifi, ssid+" ağına bağlanılamadı, yeniden deneniyor…")
		}
	}
	return "", "", errors.New("10 dakika içinde telefonun ağına bağlanılamadı")
}

// runQRPairing shows this computer's pairing code and waits until the phone has scanned it. A phone whose app
// is older than QR pairing is paired by comparing numbers instead.
func runQRPairing(ui setupUI, phoneIP, name string) (*pairing.Result, error) {
	priv, code, err := pairing.NewQR()
	if err != nil {
		return nil, err
	}
	ui.QR(code.String())
	addr := net.JoinHostPort(phoneIP, strconv.Itoa(pairing.Port))
	deadline := time.Now().Add(10 * time.Minute)
	hinted := false
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		res, err := pairing.PairQR(ctx, addr, name, priv, code)
		cancel()
		switch {
		case err == nil:
			return res, nil
		case errors.Is(err, pairing.ErrNoQR):
			ui.Step(stepPair, "Telefondaki AleShare QR ile eşleşmeyi bilmiyor (eski sürüm); numarayla eşleşiliyor.")
			if err := waitForPairingWindow(ui, phoneIP); err != nil {
				return nil, err
			}
			return runPairing(ui, phoneIP, name)
		case errors.Is(err, pairing.ErrNotScanned):
			// The app is open: waiting for the camera.
		default:
			// The app is not on the screen (or the camera has it covered for the moment).
			if !hinted {
				ui.Step(stepPair, "Telefonda AleShare uygulaması ekranda açık olmalı, paylaşım açıkken.")
				hinted = true
			}
		}
		time.Sleep(2 * time.Second)
	}
	return nil, errors.New("10 dakika içinde kod okutulmadı")
}

// lock makes a folder the administrators', open to SYSTEM and administrators only (and, with usersRead, to users
// for reading: the window runs without rights). The whole access list is replaced, not added to: an entry someone
// put there before must not survive.
func lock(dir string, usersRead bool) error {
	sddl := "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	if usersRead {
		sddl += "(A;OICI;FRFX;;;BU)"
	}
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	// The owner first: whoever owns a folder may rewrite its access list, whatever the list says. Taking a folder
	// from someone else needs a privilege that icacls switches on for itself.
	_ = command("icacls.exe", dir, "/setowner", "*S-1-5-32-544", "/L", "/Q").Run()
	err = windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
	if err != nil {
		return fmt.Errorf("%s korunamadı: %w", dir, err)
	}
	got, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("%s korunamadı: %w", dir, err)
	}
	if owner, _, err := got.Owner(); err != nil || !trustedOwner(owner) {
		return fmt.Errorf("%s başka bir hesabın ve yöneticilere devredilemedi", dir)
	}
	return nil
}

// trustedOwner reports whether owner is the administrators, SYSTEM, or the account this process runs as (which
// has administrator rights wherever this is asked).
func trustedOwner(owner *windows.SID) bool {
	if owner == nil {
		return false
	}
	if owner.IsWellKnown(windows.WinBuiltinAdministratorsSid) || owner.IsWellKnown(windows.WinLocalSystemSid) {
		return true
	}
	me, err := windows.GetCurrentProcessToken().GetTokenUser()
	return err == nil && windows.EqualSid(owner, me.User.Sid)
}

// isLink reports whether path is a link to somewhere else (a junction, a symbolic link).
func isLink(path string) bool {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(p)
	return err == nil && attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

// removeLinks takes the links out of a folder (the links themselves, not what they lead to).
func removeLinks(dir string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if path := filepath.Join(dir, e.Name()); isLink(path) {
			_ = os.Remove(path)
		}
	}
}

// makeFolders makes the program's folder and the private one inside it, and makes sure both are the
// administrators'. Any user may make a folder in %ProgramData%: one made before the installation would stay its
// maker's, with whatever was put into it, and its maker could then swap the program that the watcher service runs
// as SYSTEM. So the folders are taken over, and so is everything that lies in them already; a link standing in
// for one of them is removed, since nothing may be written through it to somewhere else.
func makeFolders() error {
	for _, dir := range []string{dataDir(), privateDir()} {
		if isLink(dir) {
			if err := os.Remove(dir); err != nil {
				return fmt.Errorf("%s bir bağlantı ve kaldırılamadı: %w", dir, err)
			}
		}
	}
	// Links inside the folder go first, and once more below when the folder is the administrators' and nobody
	// else can put one back.
	removeLinks(dataDir())
	if err := os.MkdirAll(privateDir(), 0o755); err != nil {
		return err
	}
	if err := lock(dataDir(), true); err != nil {
		return err
	}
	if err := lock(privateDir(), false); err != nil {
		return err
	}
	for _, dir := range []string{dataDir(), privateDir()} {
		removeLinks(dir)
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			if strings.EqualFold(path, privateDir()) {
				continue // has its own list, set above
			}
			if isLink(path) {
				continue // could not be removed: left alone, never followed
			}
			_ = command("icacls.exe", path, "/setowner", "*S-1-5-32-544", "/T", "/C", "/L", "/Q").Run()
			_ = command("icacls.exe", path, "/reset", "/T", "/C", "/L", "/Q").Run()
		}
	}
	return nil
}

// installSelf puts this program at installedExe. The watcher service, the icon or a window may be running the
// installed copy at this very moment, and Windows refuses to overwrite a running program but lets it be renamed:
// the old file is moved aside, the new one copied in, and what was moved aside is removed once nothing runs it any
// more (here, or at the next install). A window left open across two upgrades still runs the file the first one
// moved aside, so the second one moves under another name.
func installSelf(self string) error {
	dst := installedExe()
	if strings.EqualFold(self, dst) {
		return nil
	}
	sweepAside(dst)
	aside := ""
	if _, err := os.Stat(dst); err == nil {
		for i := 0; ; i++ {
			aside = dst + ".old"
			if i > 0 {
				aside += strconv.Itoa(i)
			}
			if _, err := os.Stat(aside); os.IsNotExist(err) {
				break
			}
			if i == 50 {
				return errors.New("eski program dosyaları silinemiyor; açık AleShare pencerelerini kapatıp yeniden dene")
			}
		}
		if err := os.Rename(dst, aside); err != nil {
			return err
		}
	}
	if err := copyFile(self, dst); err != nil {
		if aside != "" {
			_ = os.Rename(aside, dst) // keep the installation working
		}
		return err
	}
	sweepAside(dst)
	return nil
}

// sweepAside removes the program files that earlier installs moved aside and that nothing runs any more.
func sweepAside(dst string) {
	old, _ := filepath.Glob(dst + ".old*")
	for _, p := range old {
		_ = os.Remove(p)
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func run(name string, args ...string) error {
	if out, err := command(name, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func cmdSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	phoneFlag := fs.String("phone", "", "telefonun IP adresi (varsayılan: bağlı ağın ağ geçidi)")
	profile := fs.String("profile", "", "telefonun Wi-Fi ağ adı; verilirse şu an o ağa bağlı olmak gerekmez")
	pairVia := fs.String("pair-via", "", "eşleşme için telefona bu adresten ulaş (örneğin 127.0.0.1: adb forward tcp:51821 tcp:51821)")
	name := fs.String("name", defaultName(), "telefonda görünecek cihaz adı")
	autoJoin := fs.Bool("auto-join", false, "telefonun ağı görününce bilgisayar kendiliğinden ona geçsin (verilmezse açık; kapalı için -auto-join=false)")
	elevated := fs.Bool("elevated", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !isElevated() {
		fmt.Println("Kurulum yönetici izni ister. Windows'un onay penceresi açılacak...")
		return relaunchElevated("setup", args)
	}
	opts := setupOptions{phone: *phoneFlag, profile: *profile, pairVia: *pairVia, name: *name, autoJoin: *autoJoin}
	fs.Visit(func(f *flag.Flag) { opts.autoJoinGiven = opts.autoJoinGiven || f.Name == "auto-join" })
	err := doSetup(opts, consoleUI{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nHATA:", err)
	} else {
		fmt.Println("\nTelefonda paylaşım başlayınca bu bilgisayar kendiliğinden bağlanır.")
		fmt.Println("Durum için: AleShare status. Otomatik bağlanmayı istemezsen: AleShare autojoin off")
	}
	if *elevated {
		waitEnter("\nKapatmak için Enter'a bas...")
	}
	return err
}

type setupOptions struct {
	phone, profile, pairVia, name string
	autoJoin                      bool
	autoJoinGiven                 bool // the flag was on the command line: do not ask
}

func doSetup(opts setupOptions, ui setupUI) error {
	phoneFlag, profileFlag, pairVia, name := opts.phone, opts.profile, opts.pairVia, opts.name
	// The folders before anything else: WireGuard's package is downloaded into the private one, and the Wi-Fi
	// password passes through it.
	if err := makeFolders(); err != nil {
		return err
	}
	// WireGuard before the phone's network: it is downloaded, and on that network there is no internet until the
	// tunnel is up.
	wg := findWireGuard()
	if wg == "" {
		if err := installWireGuard(ui); err != nil {
			return fmt.Errorf("WireGuard kurulamadı: %w\n(Bir kez internet gerekir: bilgisayarı internetli bir ağa bağla, sonra yeniden dene.)", err)
		}
		wg = findWireGuard()
	}
	ui.Done(stepWireGuard, "WireGuard kurulu")
	// An installation that is paired anew has a watcher service running: it keeps still until this is done (see
	// holdSetup), or it would reopen the old tunnel in the middle of the pairing.
	resume := holdSetup()
	defer resume()

	var phone, profile string
	var err error
	switch {
	case phoneFlag != "" && profileFlag != "":
		// Everything is given: no need to be on the phone's network right now.
		phone, profile = phoneFlag, profileFlag
	case phoneFlag == "" && profileFlag == "":
		// The usual way: nothing is given, the computer joins the phone's own network.
		if phone, profile, err = joinPhoneWifi(ui); err != nil {
			return err
		}
	default:
		if phone, profile, err = detectPhone(phoneFlag); err != nil {
			return err
		}
		if profileFlag != "" {
			profile = profileFlag
		}
	}
	ui.Done(stepWifi, "Telefonun ağına bağlı: "+profile)
	pairHost := phone
	if pairVia != "" {
		pairHost = pairVia
	}
	// An older tunnel of this program would block the pairing (it sends everything but its own packets
	// into the tunnel), and the watcher would keep reopening it: keep both quiet until we are done. (The pause
	// below is for a watcher older than 1.2.2, which does not know holdSetup; a phone that answers ends it.)
	if running, _ := tunnelRunning(); running {
		_ = stopTunnel()
	}
	h := loadHealth()
	h.PausedUntil = time.Now().Add(15 * time.Minute)
	saveHealth(h)
	res, err := runQRPairing(ui, pairHost, name)
	if err != nil {
		return err
	}
	ui.Done(stepPair, fmt.Sprintf("Telefonda %q olarak eklendi", name))
	// Switching to the phone's network by itself is what makes it hands-off; "autojoin off" turns it off. Pairing
	// an installation anew changes its keys, not what the user chose there.
	autoJoin := opts.autoJoin
	if !opts.autoJoinGiven {
		autoJoin = true
		if old, err := loadSettings(); err == nil {
			autoJoin = old.AutoJoin
		}
	}

	ui.Step(stepInstall, "Kurulum tamamlanıyor…")
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := installSelf(self); err != nil {
		return fmt.Errorf("program kopyalanamadı: %w", err)
	}
	keepSiblingAPK(self) // the release folder's APK comes along, for the phones
	if err := os.WriteFile(confPath(), []byte(buildConf(res, phone)), 0o600); err != nil {
		return err
	}
	setMetered(profile)
	if err := saveSettings(settings{Phone: phone, Profile: profile, AutoJoin: autoJoin, Version: version, Metered: true}); err != nil {
		return err
	}
	saveNextWifi(nil) // a change the phone announced to an earlier installation is not this one's business

	// A tunnel service that only runs while the phone's network is connected.
	_ = command(wg, "/uninstalltunnelservice", tunnelName).Run()
	if err := run(wg, "/installtunnelservice", confPath()); err != nil {
		return err
	}
	if err := run("sc.exe", "config", serviceName, "start=", "demand"); err != nil {
		return err
	}

	if err := installWatchService(); err != nil {
		return fmt.Errorf("izleme hizmeti kurulamadı: %w", err)
	}
	if err := startWatchService(); err != nil {
		return fmt.Errorf("izleme hizmeti başlatılamadı: %w", err)
	}

	// Found again in the Start menu, the way any program is.
	if err := createShortcut(); err != nil {
		logf("Başlat menüsü kısayolu yazılamadı: %v", err)
	}
	if err := installTrayAutostart(); err != nil {
		logf("saatin yanındaki simge kurulamadı: %v", err)
	}

	saveHealth(health{})
	logf("kurulum tamamlandı: telefon %s, ağ %q, otomatik bağlanma %v", phone, profile, autoJoin)
	resume() // the watcher looks again, and so does this run's own first look
	if err := cmdReconcile(); err != nil {
		return fmt.Errorf("kuruldu ama ilk durum ayarı başarısız: %w", err)
	}
	ui.Done(stepInstall, "Kurulum tamamlandı")
	return nil
}

// phoneNetworkUp reports whether the phone's network is connected. Right after
// connecting, the address may still be pending, so it waits up to wait for it.
func phoneNetworkUp(set settings, wait time.Duration) (bool, error) {
	deadline := time.Now().Add(wait)
	for {
		nets, err := networks()
		if err != nil {
			return false, err
		}
		connected, profileSeen := matchPhone(nets, set.Phone, set.Profile, set.Next)
		if connected {
			return true, nil
		}
		if !profileSeen || time.Now().After(deadline) {
			return false, nil
		}
		time.Sleep(time.Second)
	}
}

// tunnelGateway is the gateway's own address inside the tunnel; it answers pings.
const tunnelGateway = "10.8.0.1"

// probeTunnel asks the phone, through the tunnel, whether anybody is there.
func probeTunnel() bool {
	out, _ := command("ping.exe", "-n", "3", "-w", "1500", tunnelGateway).CombinedOutput()
	return strings.Contains(string(out), "TTL=") // "TTL=" is not translated
}

func currentSSIDs() []string {
	out, err := command("netsh.exe", "wlan", "show", "interfaces").Output()
	if err != nil {
		return nil
	}
	return parseSSIDs(string(out))
}

// joinPhoneNetwork connects to the phone's network if it is in range. Windows rescans only every few
// minutes while it is connected, so a scan is asked for first. The connection itself raises the network
// event that makes the next run open the tunnel.
func joinPhoneNetwork(set settings) {
	h := loadHealth()
	now := time.Now()
	if h.joinQuiet(now) {
		return
	}
	_ = scanWifi()
	time.Sleep(4 * time.Second) // the scan answers asynchronously
	// Windows may have joined by itself meanwhile (a profile that connects automatically): asking again would
	// only drop that connection and make a new one.
	for _, s := range currentSSIDs() {
		if knownNetwork(set, s) {
			return
		}
	}
	out, err := command("netsh.exe", "wlan", "show", "networks").Output()
	if err != nil {
		return
	}
	ssid, ok := phoneSSID(parseVisibleSSIDs(string(out)), set.Profile, set.Next)
	if !ok {
		return
	}
	logf("telefonun ağı yakında (%s), bağlanılıyor", ssid)
	// Quiet for a while after every attempt, not only a failed one: netsh may accept the request and Windows
	// still not join (a mobile hotspot running on the same adapter, say), and each try shakes the Wi-Fi. A
	// successful join lands on the phone's network, and leaving it later resets this (see cmdReconcile).
	h.NoJoinUntil = now.Add(pauseAfterFailed)
	saveHealth(h)
	if err := run("netsh.exe", "wlan", "connect", "name="+ssid, "ssid="+ssid); err != nil {
		// No saved profile (never joined by hand, or forgotten) or a wrong password.
		logf("bağlanılamadı: %v", err)
	}
}

// lastDisconnect is Windows' latest "disconnected from a Wi-Fi network" event.
func lastDisconnect() (wlanDisconnect, bool) {
	out, err := command("wevtutil.exe", "qe", "Microsoft-Windows-WLAN-AutoConfig/Operational",
		"/q:*[System[(EventID=8003)]]", "/c:1", "/rd:true", "/f:xml").Output()
	if err != nil {
		return wlanDisconnect{}, false
	}
	return parseDisconnect(out)
}

// windowsMovedOn reports whether Windows itself left the phone's network a short while ago. It does that when a
// network has no internet of its own (here the internet comes through the tunnel, which Windows does not count)
// and a known network with internet is in range; nobody asked for it.
func windowsMovedOn(set settings) (wlanDisconnect, bool) {
	d, ok := lastDisconnect()
	return d, ok && d.Reason == reasonWindowsMovedOn && knownNetwork(set, d.Profile) && time.Since(d.At) < bringBackEvery
}

// bringBack returns to the phone's network after Windows left it by itself, whether or not the automatic switch is
// on: the user (or the switch) had put this computer there. Once in bringBackEvery at most, so that a Windows that
// insists wins instead of the two taking turns.
func bringBack(d wlanDisconnect) {
	h := loadHealth()
	now := time.Now()
	if now.Sub(h.BroughtBack) < bringBackEvery {
		return
	}
	h.BroughtBack = now
	saveHealth(h)
	logf("Windows telefonun ağından kendiliğinden ayrıldı (%s), geri dönülüyor", d.Profile)
	if err := run("netsh.exe", "wlan", "connect", "name="+d.Profile, "ssid="+d.Profile); err != nil {
		logf("geri dönülemedi: %v", err)
	}
}

// quietFlags are the flags of a command that runs with administrator rights for someone else: -elevated when the
// command line reopened it (it keeps its window open at the end), -quiet and -result when the window runs it
// (no window; the error goes to the result file).
type quietFlags struct {
	elevated, quiet *bool
	result          *string
}

func addQuietFlags(fs *flag.FlagSet) quietFlags {
	return quietFlags{fs.Bool("elevated", false, ""), fs.Bool("quiet", false, ""), fs.String("result", "", "")}
}

// needsRights reports whether the command has to reopen itself with administrator rights first; a quiet run
// was started with them, so it never does.
func (q quietFlags) needsRights() bool { return !*q.quiet && !isElevated() }

// resultName is the only name a result file may have (see runElevated).
var resultName = regexp.MustCompile(`^hotspot-result-\d+\.txt$`)

// finish ends such a command: it hands its error to the window, or waits for the user to read the console.
func (q quietFlags) finish(err error) error {
	if err != nil && resultName.MatchString(filepath.Base(*q.result)) {
		// This process has administrator rights and the path came on its command line: it writes a new file of
		// that one name and never over a file that is there.
		if f, ferr := os.OpenFile(*q.result, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644); ferr == nil {
			_, _ = f.WriteString(err.Error())
			f.Close()
		}
	}
	if *q.elevated && !*q.quiet {
		waitEnter("\nKapatmak için Enter'a bas...")
	}
	return err
}

// quietCommand runs one of the commands the window offers in this process, which has administrator rights.
func quietCommand(cmd string, args []string) error {
	switch cmd {
	case "autojoin":
		return cmdAutoJoin(args)
	case "upgrade":
		return cmdUpgrade(args)
	case "uninstall":
		return cmdUninstall(args)
	}
	return fmt.Errorf("bilinmeyen komut %q", cmd)
}

// cmdAutoJoin turns the automatic switch to the phone's network on or off.
func cmdAutoJoin(args []string) error {
	fs := flag.NewFlagSet("autojoin", flag.ContinueOnError)
	q := addQuietFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, err := loadSettings()
	if err != nil {
		return q.finish(err)
	}
	if fs.NArg() != 1 || (fs.Arg(0) != "on" && fs.Arg(0) != "off") {
		fmt.Printf("Otomatik bağlanma şu an %s. Değiştirmek için: AleShare autojoin on|off\n", onOff(set.AutoJoin))
		return nil
	}
	if q.needsRights() {
		fmt.Println("Ayar yönetici izni ister. Windows'un onay penceresi açılacak...")
		return relaunchElevated("autojoin", args)
	}
	set.AutoJoin = fs.Arg(0) == "on"
	err = saveSettings(set)
	if err == nil {
		logf("otomatik bağlanma %s", onOff(set.AutoJoin))
		if !*q.quiet {
			fmt.Printf("Otomatik bağlanma %s.\n", onOff(set.AutoJoin))
		}
	}
	return q.finish(err)
}

func onOff(b bool) string {
	if b {
		return "açık"
	}
	return "kapalı"
}

// cmdReconcile makes the tunnel match the network. The watcher service runs it after
// every change of the Wi-Fi and once a minute, so it must be idempotent and cheap when
// nothing changed.
func cmdReconcile() error {
	if setupRunning() {
		// A setup is pairing this computer anew. It closed the tunnel on purpose: the phone still answers the old
		// keys until the new pairing is in, and reopening the tunnel with them would cut the pairing off.
		return nil
	}
	defer lockReconcile()()
	set, err := loadSettings()
	if err != nil {
		return err
	}
	running, err := tunnelRunning()
	if err != nil {
		logf("%v", err)
		return err
	}
	if !running && set.Profile != "" {
		// The once-a-minute run: a quick look at the Wi-Fi is enough to see that we are elsewhere.
		ssids := currentSSIDs()
		here := false
		for _, s := range ssids {
			here = here || knownNetwork(set, s)
		}
		if !here {
			set, here = adoptNetwork(set, ssids)
		}
		if !here {
			if d, moved := windowsMovedOn(set); moved {
				bringBack(d) // and no automatic joining meanwhile: Windows would only leave again
			} else if set.AutoJoin {
				joinPhoneNetwork(set)
			}
			if len(ssids) > 0 {
				return nil
			}
		}
	}
	want, err := phoneNetworkUp(set, 20*time.Second)
	if err != nil {
		logf("%v", err)
		return err
	}
	answered := false
	if want {
		// The user may be changing the network's name or password on the phone: learn the new one now, while
		// this computer can still hear the phone, so that it follows by itself. The answer is also the quickest
		// sign of whether the phone's app is up at all.
		set, answered = followPhone(set, running)
	}

	h := loadHealth()
	now := time.Now()
	switch {
	case want && !running:
		if !answered && h.Answers && h.DeadEnds > 0 {
			// Sharing may be starting right now: the Wi-Fi group comes up a moment before the phone answers.
			time.Sleep(3 * time.Second)
			set, answered = followPhone(set, false)
		}
		wasDead := h.DeadEnds > 0
		switch h.arrive(answered, now) {
		case waitPaused:
			return nil
		case stillDead:
			logf("telefon hâlâ yanıt vermiyor: Wi-Fi'den çıkılıyor, %s sonra yeniden bakılacak", h.NoJoinUntil.Sub(now).Round(time.Second))
			saveHealth(h)
			_ = command("netsh.exe", "wlan", "disconnect").Run()
			return nil
		}
		saveHealth(h)
		if wasDead && answered {
			logf("telefon yeniden yanıt veriyor")
		}
		logf("telefonun ağına girildi, bağlantı açılıyor")
		return startTunnel()
	case !want && running:
		logf("telefonun ağından çıkıldı, bağlantı kapatılıyor")
		saveHealth(health{BroughtBack: h.BroughtBack, Answers: h.Answers}) // the rest starts afresh on the next visit
		return stopTunnel()
	case want && running:
		// On the phone's network with the tunnel up: is the phone really answering? If the app
		// died while its Wi-Fi group stayed, the tunnel would block this PC's internet for good.
		if answered {
			h.Answers = true
		}
		// The phone's answer through the tunnel is as good as a ping, and spares one.
		if h.judge(answered || probeTunnel(), now) {
			logf("telefon yanıt vermiyor (ağı açık ama paylaşım cevap vermiyor): bağlantı kapatılıyor, Wi-Fi'den çıkılıyor")
			saveHealth(h)
			err := stopTunnel()
			_ = command("netsh.exe", "wlan", "disconnect").Run() // lets Windows return to its normal network
			return err
		}
		saveHealth(h)
	}
	return nil
}

// cmdUpgrade installs this build's program and watcher service over an existing
// installation, without pairing again.
func cmdUpgrade(args []string) error {
	fs := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	q := addQuietFlags(fs)
	autoJoin := fs.String("auto-join", "", "on veya off: otomatik bağlanmayı aynı anda ayarla")
	from := fs.String("from", "", "kurulacak sürümün dosyası (imzası denetlenir); verilmezse bu dosyanın kendisi")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *autoJoin != "" && *autoJoin != "on" && *autoJoin != "off" {
		return errors.New("-auto-join on ya da off olmalı")
	}
	if _, err := loadSettings(); err != nil {
		return q.finish(err)
	}
	if q.needsRights() {
		fmt.Println("Güncelleme yönetici izni ister. Windows'un onay penceresi açılacak...")
		return relaunchElevated("upgrade", args)
	}
	var err error
	if *from != "" {
		err = upgradeFromFile(*from) // a release the window downloaded from GitHub
	} else {
		err = doUpgrade()
	}
	if err == nil && *autoJoin != "" {
		var set settings
		if set, err = loadSettings(); err == nil {
			set.AutoJoin = *autoJoin == "on"
			err = saveSettings(set)
			logf("otomatik bağlanma %s", onOff(set.AutoJoin))
		}
	}
	if *q.quiet {
		return q.finish(err)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "\nHATA:", err)
	} else {
		fmt.Println("\nGüncellendi: program ve izleme hizmeti yenilendi; eşleşme ve bağlantı ayarları aynı kaldı.")
	}
	return q.finish(err)
}

func doUpgrade() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	keepSiblingAPK(self) // the release folder's APK comes along, for the phones
	return upgradeFrom(self, version)
}

// upgradeFrom installs the program at src (release name) over the installed one, with its watcher service (which
// replaces the scheduled task of older installations). Run by the service itself (a release taken from the phone),
// the service starts again from the new file once this run is over (see watcher.Execute).
func upgradeFrom(src, name string) error {
	if !inService {
		stopWatchService()
	}
	if err := makeFolders(); err != nil {
		logf("program klasörü korunamadı: %v", err) // an installation that works is not stopped for it
	}
	if err := installSelf(src); err != nil {
		if !inService {
			_ = startWatchService()
		}
		return fmt.Errorf("program kopyalanamadı: %w", err)
	}
	if err := installWatchService(); err != nil {
		return fmt.Errorf("izleme hizmeti kurulamadı: %w", err)
	}
	if !inService {
		if err := startWatchService(); err != nil {
			return fmt.Errorf("izleme hizmeti başlatılamadı: %w", err)
		}
	}
	if set, err := loadSettings(); err == nil && (set.Version != name || !set.Metered) {
		if !set.Metered {
			setMetered(set.Profile) // installations older than 1.0.18, once
			set.Metered = true
		}
		set.Version = name
		_ = saveSettings(set)
	}
	if err := createShortcut(); err != nil {
		logf("Başlat menüsü kısayolu yazılamadı: %v", err)
	}
	if err := installTrayAutostart(); err != nil {
		logf("saatin yanındaki simge kurulamadı: %v", err)
	}
	logf("güncellendi (%s)", name)
	return nil
}

func powershell(script string) string {
	out, _ := command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	return strings.TrimSpace(string(out))
}

func cmdStatus() error {
	set, err := loadSettings()
	if err != nil {
		fmt.Println("Kurulu değil. Kurmak için: AleShare setup")
		return nil
	}
	fmt.Printf("Telefon:               %s   ağ: %q\n", set.Phone, set.Profile)
	if set.Next != "" {
		fmt.Printf("Ağın yeni adı:         %q (telefon geçiş yapıyor)\n", set.Next)
	}
	fmt.Printf("Otomatik bağlanma:     %s\n", onOff(set.AutoJoin))
	if nets, err := networks(); err == nil {
		connected, _ := matchPhone(nets, set.Phone, set.Profile, set.Next)
		fmt.Printf("Telefonun ağına bağlı: %v\n", connected)
	}
	state := powershell(`(Get-Service -Name 'WireGuardTunnel$` + tunnelName + `' -ErrorAction SilentlyContinue).Status`)
	if state == "" {
		state = "servis yok"
	}
	fmt.Printf("Bağlantı servisi:      %s\n", state)
	fmt.Printf("İzleme hizmeti:        %s\n", watchServiceState())
	if wg := findWireGuard(); wg != "" {
		fmt.Printf("WireGuard:             %s\n", wg)
	} else {
		fmt.Println("WireGuard:             kurulu değil")
	}
	if data, err := os.ReadFile(filepath.Join(dataDir(), "hotspot.log")); err == nil {
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) > 5 {
			lines = lines[len(lines)-5:]
		}
		fmt.Println("Son kayıtlar:")
		for _, l := range lines {
			fmt.Println("  " + l)
		}
	}
	return nil
}

// removeRetrying deletes a file that a program stopped a moment ago may still hold for a short while.
func removeRetrying(path string) {
	for i := 0; i < 20; i++ {
		if err := os.Remove(path); err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func cmdUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	q := addQuietFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if q.needsRights() {
		fmt.Println("Kaldırma yönetici izni ister. Windows'un onay penceresi açılacak...")
		return relaunchElevated("uninstall", args)
	}
	say := func(s string) {
		if !*q.quiet {
			fmt.Println(s)
		}
	}
	say("İzleme hizmeti ve otomatik açma görevi siliniyor...")
	removeWatchService()
	_ = command("schtasks.exe", "/Delete", "/TN", taskName, "/F").Run() // installations older than 1.0.21
	removeTray()
	// A run of the watcher that started a moment ago still holds the program file open. Only what runs as
	// SYSTEM: the window that asked for this uninstall is this program too.
	_ = command("taskkill.exe", "/F", "/FI", "IMAGENAME eq hotspot-pc.exe", "/FI", "USERNAME eq NT AUTHORITY\\SYSTEM",
		"/FI", fmt.Sprintf("PID ne %d", os.Getpid())).Run()
	say("Bağlantı kapatılıyor ve servisi siliniyor...")
	_ = stopTunnel()
	if wg := findWireGuard(); wg != "" {
		_ = command(wg, "/uninstalltunnelservice", tunnelName).Run()
	}
	say("Yapılandırma (özel anahtar dahil) siliniyor...")
	// Windows forgets the phone's network too: it was saved to be joined by itself, and without the tunnel it
	// leads nowhere. (Only a network the app opened: with -profile the setup may have been pointed at another.)
	if set, err := loadSettings(); err == nil {
		for _, ssid := range []string{set.Profile, set.Next} {
			if strings.HasPrefix(ssid, directPrefix) {
				_ = deleteWifiProfile(ssid)
			}
		}
	}
	// Everything in the folder but the program itself and what upgrades moved aside, which may still be running:
	// the key, the settings, the log, what is known of the phone, the APK kept for it.
	program := strings.ToLower(filepath.Base(installedExe()))
	entries, _ := os.ReadDir(dataDir())
	for _, e := range entries {
		if !strings.HasPrefix(strings.ToLower(e.Name()), program) {
			_ = os.RemoveAll(filepath.Join(dataDir(), e.Name()))
		}
	}
	_ = os.Remove(shortcutPath())
	_ = os.Remove(oldShortcutPath())
	removeFolderLater(onlineDir()) // the user's part: what was learned of new releases, the window's browser data
	if self, err := os.Executable(); err == nil && !strings.EqualFold(self, installedExe()) {
		removeRetrying(installedExe())
		sweepAside(installedExe())
		_ = os.Remove(dataDir())
	} else {
		removeLater() // the installed program uninstalls itself: it goes once it has ended
	}
	say("Kaldırıldı. WireGuard for Windows'a dokunulmadı (isteyen Ayarlar'dan kaldırır).")
	say("Telefonda AleShare uygulamasından bu bilgisayarı \"Kaldır\" ile silmeyi unutma.")
	return q.finish(nil)
}
