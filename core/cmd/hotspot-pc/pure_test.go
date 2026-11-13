package main

import (
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"

	"hotspot/core/internal/pairing"
	"hotspot/core/internal/wgkey"
)

func TestParseNetworks(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []netEntry
	}{
		{"empty", "", nil},
		{"single object", `{"Alias":"Wi-Fi","Profile":"DIRECT-NS-Hotspot","Gateway":"192.168.49.1"}`,
			[]netEntry{{"Wi-Fi", "DIRECT-NS-Hotspot", "192.168.49.1"}}},
		{"array with a missing gateway", `[{"Alias":"Wi-Fi","Profile":"x","Gateway":"10.0.0.1"},{"Alias":"vEthernet","Profile":null,"Gateway":null}]`,
			[]netEntry{{"Wi-Fi", "x", "10.0.0.1"}, {"vEthernet", "", ""}}},
		{"byte order mark", "\xef\xbb\xbf" + `{"Alias":"a","Profile":"b","Gateway":"c"}`, []netEntry{{"a", "b", "c"}}},
	}
	for _, c := range cases {
		got, err := parseNetworks([]byte(c.in))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(got) != len(c.want) {
			t.Fatalf("%s: got %v", c.name, got)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
			}
		}
	}
}

func TestMatchPhone(t *testing.T) {
	phoneNet := netEntry{"Wi-Fi", "DIRECT-NS-Hotspot 2", "192.168.49.1"}
	home := netEntry{"Wi-Fi", "Home", "192.168.1.1"}
	pending := netEntry{"Wi-Fi", "DIRECT-NS-Hotspot", ""}
	lookalike := netEntry{"Ethernet", "Office", "192.168.49.1"}

	if c, _ := matchPhone([]netEntry{phoneNet}, "192.168.49.1", "DIRECT-NS-Hotspot"); !c {
		t.Fatal("a renamed duplicate profile (\"... 2\") must still match")
	}
	if c, seen := matchPhone([]netEntry{home}, "192.168.49.1", "DIRECT-NS-Hotspot"); c || seen {
		t.Fatal("another network must not match")
	}
	if c, seen := matchPhone([]netEntry{pending}, "192.168.49.1", "DIRECT-NS-Hotspot"); c || !seen {
		t.Fatal("a connected network without an address yet must be reported as seen, so the caller waits")
	}
	if c, _ := matchPhone([]netEntry{lookalike}, "192.168.49.1", "DIRECT-NS-Hotspot"); c {
		t.Fatal("a foreign network that merely uses the same gateway address must not match")
	}
	if c, _ := matchPhone([]netEntry{lookalike}, "192.168.49.1", ""); !c {
		t.Fatal("without a profile name the gateway alone decides")
	}
}

func TestParseSSIDs(t *testing.T) {
	out := "\r\nThere is 1 interface on the system:\r\n\r\n" +
		"    Name                   : Wi-Fi\r\n" +
		"    State                  : connected\r\n" +
		"    SSID                   : DIRECT-HS-Q4TZ\r\n" +
		"    BSSID                  : aa:bb:cc:dd:ee:ff\r\n" +
		"    Network type           : Infrastructure\r\n"
	if got := parseSSIDs(out); len(got) != 1 || got[0] != "DIRECT-HS-Q4TZ" {
		t.Fatalf("got %q", got)
	}
	if got := parseSSIDs("    Name : Wi-Fi\r\n    State : disconnected\r\n"); len(got) != 0 {
		t.Fatalf("a disconnected interface has no SSID line, got %q", got)
	}
	// The BSSID line must not be mistaken for the SSID, nor a colon in the name cut it short.
	if got := parseSSIDs("    SSID                   : a:b\r\n    BSSID : x\r\n"); len(got) != 1 || got[0] != "a:b" {
		t.Fatalf("got %q", got)
	}
}

func TestParseVisibleSSIDs(t *testing.T) {
	out := "\r\nInterface name : Wi-Fi\r\nThere are 3 networks currently visible.\r\n\r\n" +
		"SSID 1 : EvWiFi\r\n    Network type            : Infrastructure\r\n    Authentication          : Open\r\n" +
		"    BSSID 1                 : 02:11:22:33:44:55\r\n         Signal             : 63%\r\n" +
		"SSID 2 : DIRECT-HS-Q4TZ\r\n    Network type            : Infrastructure\r\n" +
		"    BSSID 1                 : 02:66:77:88:99:aa\r\n" +
		"SSID 3 : \r\n"
	got := parseVisibleSSIDs(out)
	if len(got) != 2 || got[0] != "EvWiFi" || got[1] != "DIRECT-HS-Q4TZ" {
		t.Fatalf("got %q (the BSSID lines and the hidden network must not be listed)", got)
	}
	if s, ok := phoneSSID(got, "DIRECT-HS-Q4TZ"); !ok || s != "DIRECT-HS-Q4TZ" {
		t.Fatalf("phoneSSID = %q, %v", s, ok)
	}
	if _, ok := phoneSSID(got, "DIRECT-HS-OTHER"); ok {
		t.Fatal("another network must not be taken for the phone's")
	}
	if _, ok := phoneSSID(got, ""); ok {
		t.Fatal("without a name there is nothing to join")
	}
}

func TestGivingUpQuietsAutoJoin(t *testing.T) {
	t0 := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	var h health
	h.judge(false, t0)
	if !h.judge(false, t0.Add(time.Minute)) {
		t.Fatal("two misses must give up")
	}
	if !h.joinQuiet(t0.Add(time.Minute + pauseAfterDead/2)) {
		t.Fatal("after a dead end the PC must not walk back into the phone's network at once")
	}
	if h.joinQuiet(t0.Add(time.Minute + pauseAfterDead + time.Second)) {
		t.Fatal("the quiet time must end")
	}

	// Still dead the next time: stay away twice as long.
	t1 := t0.Add(time.Hour)
	h.judge(false, t1)
	if !h.judge(false, t1.Add(time.Minute)) {
		t.Fatal("must give up again")
	}
	if !h.joinQuiet(t1.Add(time.Minute + 2*pauseAfterDead - time.Second)) {
		t.Fatal("the second dead end must keep the PC away twice as long")
	}
	if got := deadEndPause(20); got != 2*time.Hour {
		t.Fatalf("the quiet time must stop growing at two hours, got %v", got)
	}

	// An answering phone forgives everything.
	h.judge(true, t1.Add(time.Hour))
	if h.DeadEnds != 0 {
		t.Fatal("a phone that answers must reset the count of dead ends")
	}
}

// The phone's app is updated: the watchdog gives up, the computer looks again a minute later, finds the phone
// still silent and leaves at once, and the moment the phone answers again everything is forgiven.
func TestArrivalAfterAPhoneUpdate(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)
	h := health{Answers: true}
	h.judge(false, t0)
	if !h.judge(false, t0.Add(time.Minute)) {
		t.Fatal("two misses must give up")
	}
	give := t0.Add(time.Minute)
	if h.joinQuiet(give.Add(pauseAfterDead + time.Second)) {
		t.Fatal("the first look must come after a minute, not ten")
	}

	// A minute later the app is still down: leave again, look again after twice as long.
	look := give.Add(pauseAfterDead + time.Second)
	if got := h.arrive(false, look); got != stillDead {
		t.Fatalf("silent phone after a dead end: %v", got)
	}
	if !h.joinQuiet(look.Add(2*pauseAfterDead-time.Second)) || h.joinQuiet(look.Add(2*pauseAfterDead+time.Second)) {
		t.Fatal("the next look must come after twice as long")
	}

	// Sharing started again: the phone answers, the tunnel opens at once although the watchdog's pause was on.
	back := look.Add(2*pauseAfterDead + time.Second)
	h.PausedUntil = back.Add(time.Minute)
	if got := h.arrive(true, back); got != openTunnel {
		t.Fatalf("answering phone: %v", got)
	}
	if h.DeadEnds != 0 || h.paused(back) || h.joinQuiet(back) {
		t.Fatalf("an answer must forgive everything: %+v", h)
	}
}

// A phone that never answered (an older app) is judged by the watchdog as before: silence alone is no verdict.
func TestArrivalWithAnOlderPhone(t *testing.T) {
	t0 := time.Date(2026, 11, 1, 18, 0, 0, 0, time.UTC)
	h := health{DeadEnds: 1}
	if got := h.arrive(false, t0); got != openTunnel {
		t.Fatalf("older phone: %v", got)
	}
	h.PausedUntil = t0.Add(time.Minute)
	if got := h.arrive(false, t0); got != waitPaused {
		t.Fatalf("older phone during the pause: %v", got)
	}
	// Nor is it for a phone that answers but has not been given up on: the first arrival may beat its app.
	fresh := health{Answers: true}
	if got := fresh.arrive(false, t0); got != openTunnel {
		t.Fatalf("first arrival: %v", got)
	}
}

func TestOldSettingsKeepAutoJoinOff(t *testing.T) {
	var s settings
	if err := json.Unmarshal([]byte(`{"phone":"192.168.49.1","profile":"DIRECT-HS-Q4TZ"}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.AutoJoin {
		t.Fatal("an install made before automatic joining existed must not start moving the PC around")
	}
}

func TestHealthJudge(t *testing.T) {
	t0 := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

	var h health
	if h.judge(false, t0) {
		t.Fatal("one missed probe must not give up the tunnel")
	}
	if h.judge(false, t0.Add(5*time.Second)) {
		t.Fatal("two misses of the same incident (overlapping task runs) count once")
	}
	if !h.judge(false, t0.Add(time.Minute)) {
		t.Fatal("a second miss a minute later must give up")
	}
	if !h.paused(t0.Add(time.Minute + 2*time.Minute)) {
		t.Fatal("must stay paused for a while so the tunnel does not reopen at once")
	}
	if h.paused(t0.Add(time.Minute + pauseAfterGiveUp + time.Second)) {
		t.Fatal("the pause must end")
	}

	var ok health
	ok.judge(false, t0)
	ok.judge(true, t0.Add(time.Minute))
	if ok.judge(false, t0.Add(2*time.Minute)) {
		t.Fatal("an answer in between resets the count")
	}
}

func TestBuildConf(t *testing.T) {
	res := &pairing.Result{
		Private: wgkey.Generate(),
		Server:  wgkey.Generate().Public(),
		Grant: pairing.Grant{
			IPv4: netip.MustParseAddr("10.8.0.2"), IPv6: netip.MustParseAddr("fd42:42:42::2"),
			DNS: []netip.Addr{netip.MustParseAddr("10.8.0.1"), netip.MustParseAddr("fd42:42:42::1")},
			MTU: 1420, Port: 51820,
		},
	}
	conf := buildConf(res, "192.168.49.1")
	for _, want := range []string{
		"PrivateKey = " + res.Private.Base64(),
		"Address = 10.8.0.2/32, fd42:42:42::2/128",
		"DNS = 10.8.0.1, fd42:42:42::1",
		"MTU = 1420",
		"PublicKey = " + res.Server.Base64(),
		"Endpoint = 192.168.49.1:51820",
		"AllowedIPs = 0.0.0.0/0, ::/0",
	} {
		if !strings.Contains(conf, want) {
			t.Fatalf("config lacks %q", want)
		}
	}
	if strings.Contains(conf, "PresharedKey") {
		t.Fatal("no pre-shared key may be configured")
	}
}
