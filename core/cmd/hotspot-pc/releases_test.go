package main

import (
	"testing"
	"time"
)

// Whichever side has the newer release gives it to the other; nothing moves between equals.
func TestPlanSync(t *testing.T) {
	cases := []struct {
		name                         string
		own, kept, phoneApp, phonePC int
		want                         syncPlan
	}{
		{"all the same", 12, 12, 12, 12, syncPlan{}},
		{"this computer was updated first", 13, 13, 12, 12, syncPlan{bringProgram: true, bringAPK: true}},
		{"the phone was updated first", 12, 12, 13, 13, syncPlan{takeProgram: true, takeAPK: true}},
		{"a tablet brought the phone a newer app only", 12, 12, 13, 12, syncPlan{takeAPK: true}},
		{"the phone has no program yet", 12, 12, 12, 0, syncPlan{bringProgram: true}},
		{"an older phone app says nothing", 12, 12, 0, 0, syncPlan{bringProgram: true}},
		{"a development build takes nothing over itself", 0, 0, 13, 13, syncPlan{takeAPK: true}},
	}
	for _, c := range cases {
		if got := planSync(c.own, c.kept, c.phoneApp, c.phonePC); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

// A transfer the phone turned away waits half an hour before it is tried again; another release goes at once.
func TestTransferRetry(t *testing.T) {
	var s releaseState
	t0 := time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)
	if !s.due("bring-apk", 12, t0) {
		t.Fatal("first try held back")
	}
	if s.due("bring-apk", 12, t0.Add(time.Minute)) {
		t.Fatal("tried again a minute later")
	}
	if !s.due("bring-apk", 13, t0.Add(time.Minute)) {
		t.Fatal("a newer release held back")
	}
	if !s.due("bring-apk", 12, t0.Add(31*time.Minute)) {
		t.Fatal("never tried again")
	}
}

func TestReleaseNames(t *testing.T) {
	for name, want := range map[string]bool{"AleShare.apk": true, "AleShare (1).apk": true, "AleShare-1.1.apk": false, "aleshare.apk.part": false, "Other.apk": false} {
		if got := publishedAPK.MatchString(name); got != want {
			t.Errorf("publishedAPK(%q) = %v", name, got)
		}
	}
	for name, want := range map[string]int{"AleShare-1.1.apk": 39, "AleShare-1.1.2.apk": 41, "AleShare-1.0.32.apk": 32, "AleShare-1.0.40.apk": 0, "Hotspot-1.0.12.apk": 12, "Hotspot-1.0.9.apk": 9, "Hotspot.apk": 0, "hotspot-pc.exe": 0, "Hotspot-1.0.12.apk.part": 0} {
		if got := apkRelease(name); got != want {
			t.Errorf("apkRelease(%q) = %d", name, got)
		}
	}
	for v, want := range map[string]int{"1.1": 39, "1.1.1": 40, "1.1.12": 51, "1.1.0": 0, "1.1.01": 0, "1.0.39": 0, "1.2": 0, "1.0.12": 12, "1.0.0": 0, "geliştirme": 0, "1.0.12-x": 0} {
		if got := versionCode(v); got != want {
			t.Errorf("versionCode(%q) = %d", v, got)
		}
	}
}
