package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The download page as it was on 1 November 2026, with an older and a newer release added to it.
const listing = `<ul><li><a href="wireguard-installer.exe">wireguard-installer.exe</a></li></ul><h3>WireGuard MSIs</h3><ul>` +
	`<li><a href="wireguard-amd64-0.5.3.msi">wireguard-amd64-0.5.3.msi</a></li>` +
	`<li><a href="wireguard-amd64-1.1.1.msi">wireguard-amd64-1.1.1.msi</a></li>` +
	`<li><a href="wireguard-amd64-1.10.0.msi">wireguard-amd64-1.10.0.msi</a></li>` +
	`<li><a href="wireguard-arm64-1.1.1.msi">wireguard-arm64-1.1.1.msi</a></li>` +
	`<li><a href="wireguard-x86-1.1.1.msi">wireguard-x86-1.1.1.msi</a></li></ul>`

func TestNewestMSI(t *testing.T) {
	cases := map[string]string{"amd64": "wireguard-amd64-1.10.0.msi", "arm64": "wireguard-arm64-1.1.1.msi", "386": "wireguard-x86-1.1.1.msi"}
	for arch, want := range cases {
		if got, ok := newestMSI(listing, arch); !ok || got != want {
			t.Errorf("%s: got %q, want %q", arch, got, want)
		}
	}
	if _, ok := newestMSI(listing, "riscv64"); ok {
		t.Error("found a package for an architecture WireGuard does not build")
	}
	if _, ok := newestMSI("<html>maintenance</html>", "amd64"); ok {
		t.Error("found a package in a page without any")
	}
}

// Every Wi-Fi Direct network once (the app's names no longer say "HS"), those of the old form first.
func TestPhoneNetworks(t *testing.T) {
	got := phoneNetworks([]string{"EvAgi", "DIRECT-Ofis", "DIRECT-HS-K7PM", "DIRECT-xy-Printer", "DIRECT-HS-K7PM", "DIRECT-_x", "DIRECT-E"})
	if strings.Join(got, ",") != "DIRECT-HS-K7PM,DIRECT-Ofis,DIRECT-xy-Printer" {
		t.Fatalf("got %v", got)
	}
}

// The code is square, has its quiet zone, fits an ordinary console, and draws the same modules either way.
func TestQRRows(t *testing.T) {
	text := "HSPC1:" + strings.Repeat("A", 64)
	color, err := qrRows(text, true)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := qrRows(text, false)
	if len(color) != len(plain) || len(color) > 30 {
		t.Fatalf("%d and %d lines", len(color), len(plain))
	}
	width := utf8.RuneCountInString(plain[0])
	if width > 60 || width < 2*len(plain)-2 || width > 2*len(plain) {
		t.Fatalf("width %d for %d lines: not square", width, len(plain))
	}
	if strings.TrimSpace(strings.ReplaceAll(plain[0], "█", "")) != "" {
		t.Fatal("the first line is not all quiet zone")
	}
	if !strings.HasPrefix(color[0], "\x1b[30;107m") || !strings.HasSuffix(color[0], "\x1b[0m") {
		t.Fatal("colored lines must set and reset their colors")
	}
	// Ink is swapped between the two: a full block in one is a blank in the other (all but the odd last line).
	c := []rune(strings.TrimSuffix(strings.TrimPrefix(color[3], "\x1b[30;107m"), "\x1b[0m"))
	p := []rune(plain[3])
	swap := map[rune]rune{'█': ' ', ' ': '█', '▀': '▄', '▄': '▀'}
	for i := range p {
		if swap[c[i]] != p[i] {
			t.Fatalf("column %d: %q and %q", i, c[i], p[i])
		}
	}
}
