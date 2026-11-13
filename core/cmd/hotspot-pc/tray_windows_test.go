package main

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func titles(alerts []trayAlert) string {
	var t []string
	for _, a := range alerts {
		t = append(t, a.title)
	}
	return strings.Join(t, " | ")
}

func TestTrayTellsEachThingOnce(t *testing.T) {
	var a trayAlerts
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	phone := &phoneStatus{Battery: 60, Devices: []phoneDevice{{Name: "PC", Paused: "none"}}}
	if got := a.check(true, phone, "PC", now); len(got) != 0 {
		t.Fatalf("the first look only learns: %s", titles(got))
	}

	// A short break says nothing; a long one says it once, and the return is told.
	a.check(false, nil, "PC", now.Add(5*time.Second))
	if got := a.check(false, nil, "PC", now.Add(15*time.Second)); len(got) != 0 {
		t.Fatalf("10 s down: %s", titles(got))
	}
	if got := a.check(false, nil, "PC", now.Add(30*time.Second)); titles(got) != "Telefonla bağlantı yok" {
		t.Fatalf("25 s down: %q", titles(got))
	}
	if got := a.check(false, nil, "PC", now.Add(40*time.Second)); len(got) != 0 {
		t.Fatalf("told twice: %s", titles(got))
	}
	if got := a.check(true, phone, "PC", now.Add(50*time.Second)); titles(got) != "Telefona yeniden bağlandı" {
		t.Fatalf("back: %q", titles(got))
	}

	low := &phoneStatus{Battery: 14, Hot: true, Devices: phone.Devices}
	if got := titles(a.check(true, low, "PC", now)); got != "Telefonun pili %14 | Telefon ısındı" {
		t.Fatalf("low and hot: %q", got)
	}
	if got := a.check(true, low, "PC", now); len(got) != 0 {
		t.Fatalf("told twice: %s", titles(got))
	}

	plan := &phoneStatus{Battery: 50, Plan: &phonePlan{LimitMB: 1000, Used: 950 << 20, DaysLeft: 3},
		Devices: []phoneDevice{{Name: "PC", Paused: "quiet"}, {Name: "Tab", Paused: "manual"}}}
	if got := titles(a.check(true, plan, "PC", now)); got != "Aylık paketin %90'ı kullanıldı | Bu bilgisayarın interneti duraklatıldı" {
		t.Fatalf("plan and pause: %q", got)
	}
	plan.Plan.Used = 1000 << 20
	if got := titles(a.check(true, plan, "PC", now)); got != "Aylık paket bitti" {
		t.Fatalf("plan used up: %q", got)
	}

	limit := &phoneStatus{Battery: 50, LimitMB: 1024, Today: 950 << 20}
	if got := titles(a.check(true, limit, "PC", now)); got != "Günlük veri sınırı dolmak üzere" {
		t.Fatalf("daily limit: %q", got)
	}
	if got := a.check(true, limit, "PC", now.Add(time.Hour)); len(got) != 0 {
		t.Fatalf("told twice the same day: %s", titles(got))
	}
}

func TestTrayLockHandsOver(t *testing.T) {
	if h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(trayMutex)); err == nil {
		windows.CloseHandle(h)
		t.Skip("a tray icon runs in this session")
	}
	first, ok := trayLock(0)
	if !ok {
		t.Fatal("no icon runs, yet the lock was refused")
	}
	if _, ok := trayLock(0); ok {
		t.Fatal("a second icon got the lock")
	}
	time.AfterFunc(500*time.Millisecond, func() { windows.CloseHandle(first) })
	start := time.Now()
	second, ok := trayLock(5 * time.Second)
	if !ok {
		t.Fatal("the next icon did not take over")
	}
	windows.CloseHandle(second)
	if d := time.Since(start); d < 400*time.Millisecond || d > 3*time.Second {
		t.Fatalf("took over after %v", d)
	}
}

func TestTrayTip(t *testing.T) {
	if got := trayTip(false, nil); got != "AleShare · bağlı değil" {
		t.Fatal(got)
	}
	p := &phoneStatus{DownBps: 3_400_000, UpBps: 180_000, Battery: 48, Network: "5G"}
	want := "AleShare · bağlı\n↓ 27 Mbit/s  ↑ 1,4 Mbit/s\nTelefon %48 · 5G"
	if got := trayTip(true, p); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if n := len([]rune(want)); n > 127 {
		t.Fatalf("%d characters: too long for a tooltip", n)
	}
}

// Signing in away from the phone is no lost connection: nothing was connected yet.
func TestTraySaysNothingBeforeTheFirstConnection(t *testing.T) {
	var a trayAlerts
	now := time.Date(2026, 11, 2, 8, 0, 0, 0, time.Local)
	for i := 0; i < 40; i++ {
		if got := a.check(false, nil, "PC", now.Add(time.Duration(i)*3*time.Second)); len(got) != 0 {
			t.Fatalf("after %d s without ever being connected: %s", i*3, titles(got))
		}
	}
	a.check(true, nil, "PC", now.Add(3*time.Minute))
	a.check(false, nil, "PC", now.Add(4*time.Minute))
	if got := a.check(false, nil, "PC", now.Add(5*time.Minute)); titles(got) != "Telefonla bağlantı yok" {
		t.Fatalf("lost after it was up: %q", titles(got))
	}
}
