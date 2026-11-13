package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// stepID names a part of the setup, in the order the user sees them.
type stepID string

const (
	stepWireGuard stepID = "wireguard" // the tunnel program
	stepWifi      stepID = "wifi"      // the phone's own network
	stepPair      stepID = "pair"      // the QR code (or the number)
	stepInstall   stepID = "install"   // files, tunnel service, scheduled task
)

// setupUI is where the setup says what it is doing and asks what only the user knows: the window, or the console
// for the command line.
type setupUI interface {
	// Step says that a part of the setup is under way, with a line about what it waits for.
	Step(id stepID, detail string)
	// Done says that a part is finished, with what came of it.
	Done(id stepID, detail string)
	// Password asks for the Wi-Fi password of the phone's network; retry is set when the last one did not work.
	// It reports false when the user gave up the setup instead.
	Password(ssid string, retry bool) (string, bool)
	// Choose asks which of several of the phone's networks is meant.
	Choose(question string, options []string) string
	// QR shows the pairing code until the phone has read it.
	QR(code string)
	// Number shows the six digits to compare, for a phone whose app is older than QR pairing.
	Number(sas string)
	// Out takes the output of the programs the setup runs (winget); the window drops it.
	Out() io.Writer
}

// consoleUI is the setup on the command line.
type consoleUI struct{}

func (consoleUI) Step(id stepID, detail string) { fmt.Println(detail) }

func (consoleUI) Done(id stepID, detail string) { fmt.Println("✓ " + detail) }

func (consoleUI) Password(ssid string, retry bool) (string, bool) {
	if retry {
		fmt.Println("Bağlanılamadı; parola yanlış olabilir (8-63 karakter).")
	}
	fmt.Printf("\nTelefonun ağı bulundu: %s\n", ssid)
	return readHidden("Parolasını yaz (telefonda AleShare > Wi-Fi ağı kartında, göz simgesi), Enter: "), true
}

// errCancelled ends a setup the user gave up in the window.
var errCancelled = errors.New("kurulum iptal edildi")

func (consoleUI) Choose(question string, options []string) string {
	fmt.Println(question)
	for i, o := range options {
		fmt.Printf("  %d) %s\n", i+1, o)
	}
	for {
		fmt.Print("Numara: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if n, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && n >= 1 && n <= len(options) {
			return options[n-1]
		}
	}
}

func (consoleUI) QR(code string) {
	fmt.Println("\nTelefonda AleShare > Paylaş > Cihazlar > Ekle > Bilgisayar'a dokun ve kamerayı bu koda tut:")
	if err := showQR(code); err != nil {
		fmt.Println("QR kod çizilemedi:", err)
	}
	fmt.Println("Kod yalnızca bu eşleşme içindir; okutunca bu bilgisayar kendiliğinden eklenir. Bekliyorum...")
}

func (consoleUI) Number(sas string) {
	fmt.Printf("\n  Eşleşme numarası:  %s\n\n  Telefonda aynı numara görünüyorsa \"Eşleştir\"e dokun.\n  Numara farklıysa veya bu isteği sen başlatmadıysan \"Reddet\"e dokun.\n\n", sas)
}

func (consoleUI) Out() io.Writer { return os.Stdout }
