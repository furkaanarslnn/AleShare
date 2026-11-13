package main

import (
	"fmt"
	"strings"

	"github.com/skip2/go-qrcode"
)

// qrSVG draws a QR code as SVG for the window: the dark modules as one path on white, quiet zone included, sharp
// at any size.
func qrSVG(text string) (string, error) {
	q, err := qrcode.New(text, qrcode.Medium)
	if err != nil {
		return "", err
	}
	bm := q.Bitmap()
	var path strings.Builder
	for y, row := range bm {
		for x := 0; x < len(row); {
			if !row[x] {
				x++
				continue
			}
			start := x
			for x < len(row) && row[x] {
				x++
			}
			fmt.Fprintf(&path, "M%d %dh%dv1h-%dz", start, y, x-start, x-start)
		}
	}
	n := len(bm)
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`+
		`<rect width="%d" height="%d" fill="#fff"/><path d="%s" fill="#101820"/></svg>`, n, n, n, n, path.String()), nil
}

// tunnel service states as the window shows them.
const (
	serviceRunning = "running"
	serviceStopped = "stopped"
	serviceMissing = "missing"
)

// connView is what the window says about the connection: a tone ("ok", "warn", "idle"), a short label, a
// headline and a sentence.
type connView struct {
	Tone     string `json:"tone"`
	Label    string `json:"label"`
	Headline string `json:"headline"`
	Body     string `json:"body"`
}

// describe turns the installation's state into what the window says.
func describe(set settings, onNetwork bool, tunnel string, h health) connView {
	switch {
	case tunnel == serviceMissing:
		return connView{"warn", "Eksik", "Kurulum tamamlanmamış", "Bağlantı kurulamıyor. Gelişmiş bölümünden yeniden eşleştir."}
	case onNetwork && tunnel == serviceRunning:
		return connView{"ok", "Bağlı", "Telefona bağlı", "Bu bilgisayar interneti telefonun mobil verisinden alıyor."}
	case onNetwork && h.DeadEnds > 0:
		return connView{"warn", "Yanıt yok", "Telefon yanıt vermiyor", "Telefonun Wi-Fi ağı görünüyor ama paylaşım yanıt vermiyor. Telefonda paylaşımın açık olduğuna bak; bilgisayar birazdan yeniden dener."}
	case onNetwork:
		return connView{"warn", "Bağlanıyor", "Telefona bağlanıyor", "İnternet birkaç saniye içinde gelir."}
	case set.AutoJoin:
		return connView{"idle", "Beklemede", "Telefon bekleniyor", "Telefonda paylaşımı başlatınca bu bilgisayar kendiliğinden bağlanır (" + set.Profile + ")."}
	default:
		return connView{"idle", "Beklemede", "Telefon bekleniyor", "Wi-Fi listesinden " + set.Profile + " ağına bağlanınca internet kendiliğinden gelir."}
	}
}
