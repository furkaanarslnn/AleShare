package main

import (
	"strings"
	"testing"
)

// The window's QR code is the same code as the console's, drawn as SVG: square, quiet zone included, dark on white.
func TestQRSVG(t *testing.T) {
	// A real code's text: base64url, mixed case, so the QR code is in byte mode.
	svg, err := qrSVG("HSPC1:x9Kd2mQ7vN4pL8sR3tY6wZ1aB5cE0fG2hJ4kM7nP9qS3uV6xY8zA1bC4dE7fG0hJ")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(svg, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 45 45"`) {
		t.Fatalf("not a 45-module square (version 5 and the quiet zone): %.80s", svg)
	}
	if !strings.Contains(svg, `fill="#fff"`) || !strings.Contains(svg, `fill="#101820"`) || strings.Count(svg, "M") < 50 {
		t.Fatal("no dark modules on white")
	}
	// The quiet zone: no dark square starts in the first four rows or columns.
	if strings.Contains(svg, "M0 ") || strings.Contains(svg, " 0h") {
		t.Fatal("dark modules in the quiet zone")
	}
}

func TestDescribe(t *testing.T) {
	set := settings{Profile: "DIRECT-HS-K7PM", AutoJoin: true}
	cases := []struct {
		name      string
		set       settings
		onNetwork bool
		tunnel    string
		h         health
		tone      string
		label     string
	}{
		{"connected", set, true, serviceRunning, health{}, "ok", "Bağlı"},
		{"joining", set, true, serviceStopped, health{}, "warn", "Bağlanıyor"},
		{"phone silent", set, true, serviceStopped, health{DeadEnds: 2}, "warn", "Yanıt yok"},
		{"away, switches by itself", set, false, serviceStopped, health{}, "idle", "Beklemede"},
		{"no tunnel service", set, false, serviceMissing, health{}, "warn", "Eksik"},
	}
	for _, c := range cases {
		v := describe(c.set, c.onNetwork, c.tunnel, c.h)
		if v.Tone != c.tone || v.Label != c.label || v.Headline == "" || v.Body == "" {
			t.Errorf("%s: %+v", c.name, v)
		}
	}
	manual := describe(settings{Profile: "DIRECT-HS-K7PM"}, false, serviceStopped, health{})
	auto := describe(set, false, serviceStopped, health{})
	if manual.Body == auto.Body || !strings.Contains(manual.Body, "DIRECT-HS-K7PM") {
		t.Errorf("waiting texts: %q / %q", manual.Body, auto.Body)
	}
}
