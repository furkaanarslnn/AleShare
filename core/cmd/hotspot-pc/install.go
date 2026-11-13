package main

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/skip2/go-qrcode"
)

// wireGuardDownloads lists the official WireGuard for Windows packages, signed by WireGuard LLC.
const wireGuardDownloads = "https://download.wireguard.com/windows-client/"

var msiName = regexp.MustCompile(`wireguard-(amd64|arm64|x86)-([0-9]+(?:\.[0-9]+)*)\.msi`)

// newestMSI picks the newest package for this computer's architecture (Go's name for it) from the download
// page's listing.
func newestMSI(listing, goarch string) (string, bool) {
	arch := map[string]string{"amd64": "amd64", "arm64": "arm64", "386": "x86"}[goarch]
	best, bestVersion := "", []int(nil)
	for _, m := range msiName.FindAllStringSubmatch(listing, -1) {
		if m[1] != arch {
			continue
		}
		v := versionParts(m[2])
		if best == "" || newerVersion(v, bestVersion) {
			best, bestVersion = m[0], v
		}
	}
	return best, best != ""
}

func versionParts(s string) []int {
	var out []int
	for _, p := range strings.Split(s, ".") {
		n, _ := strconv.Atoi(p)
		out = append(out, n)
	}
	return out
}

func newerVersion(a, b []int) bool {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// oldAppPrefix started every name of the app's network up to 1.0.30; such a network is surely a phone's.
const oldAppPrefix = "DIRECT-HS-"

// phoneNetworks keeps, once each, the visible networks that the app could have opened: Wi-Fi Direct networks,
// whose names Android starts with "DIRECT-" and two letters or digits (see WifiNetwork.PREFIX in the app). A
// printer's Wi-Fi Direct network looks the same, so names of the app's old form come first, then the others in
// the order seen; the setup asks the user when there are several.
func phoneNetworks(visible []string) []string {
	var old, others []string
	seen := map[string]bool{}
	for _, s := range visible {
		if !wifiDirectName(s) || seen[s] {
			continue
		}
		seen[s] = true
		if strings.HasPrefix(s, oldAppPrefix) {
			old = append(old, s)
		} else {
			others = append(others, s)
		}
	}
	return append(old, others...)
}

// wifiDirectName reports whether s is a Wi-Fi Direct network's name: "DIRECT-" and two letters or digits.
func wifiDirectName(s string) bool {
	rest, ok := strings.CutPrefix(s, "DIRECT-")
	if !ok || len(rest) < 2 {
		return false
	}
	for _, c := range rest[:2] {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// qrRows draws a QR code for a console, two rows of modules per line with half blocks. With color, dark modules
// are black on white whatever the console's colors (ANSI sequences); without, the light modules are drawn in the
// console's own text color, which reads right on the usual dark background.
func qrRows(text string, color bool) ([]string, error) {
	q, err := qrcode.New(text, qrcode.Medium)
	if err != nil {
		return nil, err
	}
	bm := q.Bitmap() // true is dark, with the quiet zone around it
	var rows []string
	for y := 0; y < len(bm); y += 2 {
		var b strings.Builder
		if color {
			b.WriteString("\x1b[30;107m")
		}
		for x := range bm[y] {
			// Which halves are drawn in the "ink" color: dark modules with color, light ones without. Below the
			// last row (the size is odd) nothing is drawn.
			last := y+1 >= len(bm)
			inkTop, inkBottom := bm[y][x], !last && bm[y+1][x]
			if !color {
				inkTop, inkBottom = !bm[y][x], !last && !bm[y+1][x]
			}
			switch {
			case inkTop && inkBottom:
				b.WriteString("█")
			case inkTop:
				b.WriteString("▀")
			case inkBottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		if color {
			b.WriteString("\x1b[0m")
		}
		rows = append(rows, b.String())
	}
	return rows, nil
}
