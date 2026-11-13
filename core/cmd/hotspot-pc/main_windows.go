// Command hotspot-pc (AleShare for Windows, built as AleShare.exe) connects this Windows PC to a sharing phone
// once and then keeps the connection automatic: the WireGuard tunnel opens when the PC joins
// the phone's network and closes when it leaves.
//
// It uses the official WireGuard for Windows for the tunnel itself, and installs it
// if it is missing. Pairing needs no file: this PC makes its own key pair and shows
// a QR code with its public key and a one-time secret, which the phone's camera
// reads; the secret proves to each side that nobody sits in between. (A phone with
// an older app compares a six-digit number instead.)
//
// Started without arguments (a double click) it opens its window (HTML drawn by Edge
// WebView2, see ui/index.html), which walks through the setup or shows the state and
// the settings. The commands below are for the command line and the scheduled task.
package main

import (
	"fmt"
	"os"
)

func usage() {
	fmt.Fprint(os.Stderr, `kullanım:
  AleShare              (çift tıkla) AleShare penceresi: kurulum, durum, ayarlar, güncelleme ve kaldırma
  AleShare setup        tek seferlik kurulum: WireGuard'ı kurar, telefonun ağına katılır, QR kodla eşleşir,
                        tüneli ve izleme hizmetini kurar (yönetici izni ister)
                        bayraklar: -phone IP  -profile WIFI_ADI  -pair-via ADRES  -name AD  -auto-join
  AleShare pair         yalnızca eşleşir ve bir WireGuard yapılandırması yazar, kurulum yapmaz
  AleShare status       durumu gösterir
  AleShare check        GitHub'da daha yeni bir sürüm var mı diye bakar
  AleShare licenses     programın yapıldığı açık kaynak bileşenlerin lisans metinlerini yazar
  AleShare autojoin on|off     telefonun ağı görününce bilgisayar kendiliğinden ona geçsin (yönetici izni ister)
  AleShare reconcile    (izleme hizmeti çalıştırır) telefonun ağındaysa tüneli açar, değilse kapatır
  AleShare upgrade      bu sürümün programını ve izleme hizmetini mevcut kurulumun üzerine yazar, yeniden eşleşmez (yönetici izni ister)
  AleShare uninstall    her şeyi kaldırır (yönetici izni ister)
`)
}

// consoleWizard is the setup in a console, for a Windows without the Edge WebView2 runtime the window needs
// (Windows 11 always has it; Windows 10 nearly always).
func consoleWizard(why error) {
	useConsole(true)
	fmt.Printf("AleShare penceresi açılamadı (%v); kurulum burada sürüyor.\n\n", why)
	err := wizard()
	if err != nil {
		fmt.Fprintln(os.Stderr, "HATA:", err)
	}
	pauseIfOwnConsole()
	if err != nil {
		os.Exit(1)
	}
}

// wizard is the console's setup, or the state if it is set up.
func wizard() error {
	if _, err := loadSettings(); err != nil {
		fmt.Println("AleShare kurulumu")
		fmt.Println("1. Telefonda AleShare uygulamasını aç ve Paylaşımı başlat'a dokun.")
		fmt.Println("2. Kurulum yönetici izni ister; Windows'un onay penceresini onayla ve açılan pencereyi izle.")
		fmt.Println("   Gerekirse WireGuard'ı indirip kurar (bir kez internet gerekir), telefonun Wi-Fi ağını bulur")
		fmt.Println("   (bilmiyorsa parolasını bir kez sorar) ve bir QR kod gösterir.")
		fmt.Println("3. Telefonda Cihazlar > Ekle > Bilgisayar'a dokun, kamerayı koda tut. Gerisi kendiliğinden.")
		fmt.Println()
		return cmdSetup(nil)
	}
	if err := cmdStatus(); err != nil {
		return err
	}
	fmt.Println("\nAleShare kurulu. Telefonda paylaşımı başlatıp bu bilgisayarı telefonun ağına bağlaman yeterli; internet kendiliğinden gelir.")
	fmt.Println("Başka bir şey için bir komut istemcisinden: AleShare yardım (upgrade, autojoin, uninstall)")
	return nil
}

func main() {
	cmd, args := "", []string(nil)
	if len(os.Args) >= 2 {
		cmd, args = os.Args[1], os.Args[2:]
	}
	switch cmd {
	case "", "window":
		// A double click: the window. "window -setup" is the window reopened with administrator rights to set up.
		if err := runWindow(cmd == "window" && len(args) > 0 && args[0] == "-setup"); err != nil {
			consoleWizard(err)
		}
		return
	case "tray":
		// The icon next to the clock, started at sign-in (see tray_windows.go).
		_ = cmdTray(args)
		return
	case "service":
		// The watcher service, started by Windows (see watch_windows.go).
		if err := cmdService(); err != nil {
			logf("izleme hizmeti çalışamadı: %v", err)
			os.Exit(1)
		}
		return
	}
	useConsole(isElevatedRelaunch())
	var err error
	switch cmd {
	case "help", "yardim", "-h", "--help":
		usage()
	case "setup":
		err = cmdSetup(args)
	case "pair":
		err = cmdPair(args)
	case "status":
		err = cmdStatus()
	case "autojoin":
		err = cmdAutoJoin(args)
	case "reconcile":
		err = cmdReconcile()
	case "uninstall":
		err = cmdUninstall(args)
	case "upgrade":
		err = cmdUpgrade(args)
	case "check":
		err = cmdCheck()
	case "licenses":
		fmt.Print(licensesText)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "HATA:", err)
		os.Exit(1)
	}
}
