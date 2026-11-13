package main

import (
	"strings"
	"testing"
	"time"
)

var goodManifest = `{"code":40,"name":"1.1.1",
 "apk":{"name":"AleShare.apk","url":"https://github.com/furkaanarslnn/AleShare/releases/download/v1.1.1/AleShare.apk","size":25000000,"sha256":"` + sum64 + `"},
 "pc":{"name":"AleShare.exe","url":"https://github.com/furkaanarslnn/AleShare/releases/download/v1.1.1/AleShare.exe","size":8500000,"sha256":"` + sum64 + `"}}`

var sum64 = strings.Repeat("ab", 32)

func TestParseManifest(t *testing.T) {
	r, err := parseManifest([]byte("\xef\xbb\xbf" + goodManifest))
	if err != nil {
		t.Fatal(err)
	}
	if r.Code != 40 || r.Name != "1.1.1" || r.PC.Size != 8500000 || r.APK.Name != "AleShare.apk" {
		t.Fatalf("got %+v", r)
	}
	bad := map[string]string{
		"a file on another site":        strings.Replace(goodManifest, "https://github.com/furkaanarslnn/AleShare/releases/download/v1.1.1/AleShare.exe", "https://example.com/AleShare.exe", 1),
		"a file in another repository":  strings.Replace(goodManifest, "furkaanarslnn/AleShare/releases/download/v1.1.1/AleShare", "someone/AleShare/releases/download/v1.1.1/AleShare", 1),
		"a name that is not the number": strings.Replace(goodManifest, `"name":"1.1.1"`, `"name":"2.0"`, 1),
		"no size":                       strings.Replace(goodManifest, `"size":8500000`, `"size":0`, 1),
		"a short checksum":              strings.Replace(goodManifest, sum64+`"}}`, `abcd"}}`, 1),
		"not JSON":                      "<html>Not Found</html>",
	}
	for what, text := range bad {
		if _, err := parseManifest([]byte(text)); err == nil {
			t.Errorf("accepted %s", what)
		}
	}
}

func TestOnlineOfferAndAnnouncement(t *testing.T) {
	rel, _ := parseManifest([]byte(goodManifest))
	s := onlineState{Release: &rel}
	if s.offer(39) == nil || s.offer(40) != nil || s.offer(41) != nil {
		t.Fatal("only an older installation is offered the release")
	}
	if s.offer(0) != nil {
		t.Fatal("a build that is no release must be offered nothing")
	}
	if (onlineState{}).offer(1) != nil {
		t.Fatal("nothing known, nothing offered")
	}
	if s.announce(39) == nil {
		t.Fatal("a new release must be told")
	}
	s.Told = 40
	if s.announce(39) != nil {
		t.Fatal("a release is told once")
	}
	newer := rel
	newer.Code, newer.Name = 41, "1.1.2"
	s.Release = &newer
	if r := s.announce(39); r == nil || r.Code != 41 {
		t.Fatal("the next release must be told again")
	}
}

func TestOnlineLooksAreSpaced(t *testing.T) {
	t0 := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	if !(onlineState{}).due(t0) {
		t.Fatal("never looked: look")
	}
	s := onlineState{Checked: t0, Tried: t0}
	if s.due(t0.Add(11*time.Hour)) || !s.due(t0.Add(12*time.Hour+time.Second)) {
		t.Fatal("an answered look holds for twelve hours")
	}
	// Half a day later there is no internet: try again in half an hour, not every few seconds.
	failed := onlineState{Checked: t0, Tried: t0.Add(13 * time.Hour)}
	if failed.due(t0.Add(13*time.Hour+10*time.Minute)) || !failed.due(t0.Add(13*time.Hour+31*time.Minute)) {
		t.Fatal("a look without an answer is tried again after half an hour")
	}
	if !s.due(t0.Add(-time.Hour)) {
		t.Fatal("a clock set back must not silence the looks")
	}
}
