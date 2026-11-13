package main

import (
	"encoding/xml"
	"reflect"
	"strings"
	"testing"

	"hotspot/core/internal/wifiinfo"
)

var (
	netA  = wifiinfo.Network{SSID: "DIRECT-HS-Q4TZ", Password: "abcdEFGH2345jkmn"}
	netB  = wifiinfo.Network{SSID: "DIRECT-HS-Ev", Password: "yeni-parola-123"}
	netA2 = wifiinfo.Network{SSID: "DIRECT-HS-Q4TZ", Password: "sadece-parola-9"}
)

func ptr(n wifiinfo.Network) *wifiinfo.Network { return &n }

// A whole change of name, as the computer lives through it: told the new network, then on it.
func TestFollowRename(t *testing.T) {
	set := settings{Phone: "192.168.49.1", Profile: "DIRECT-HS-Q4TZ", AutoJoin: true}

	// Nothing changes: nothing to do.
	s, applied, p := planFollow(set, nil, wifiinfo.Info{Current: netA})
	if s != set || applied != nil || len(p.add)+len(p.remove) != 0 {
		t.Fatalf("idle: %+v %v %+v", s, applied, p)
	}

	// The phone announces the new network: Windows learns it, the computer is at home on both.
	s, applied, p = planFollow(set, nil, wifiinfo.Info{Current: netA, Next: &netB})
	if s.Profile != netA.SSID || s.Next != netB.SSID || applied == nil || *applied != netB {
		t.Fatalf("announced: %+v %v", s, applied)
	}
	if !reflect.DeepEqual(p.add, []wifiinfo.Network{netB}) || len(p.remove) != 0 {
		t.Fatalf("announced plan: %+v", p)
	}
	if !knownNetwork(s, "DIRECT-HS-Q4TZ") || !knownNetwork(s, "DIRECT-HS-Ev 2") || knownNetwork(s, "DIRECT-xy-Other") {
		t.Fatal("knownNetwork")
	}

	// Asked again before the switch: already done.
	s2, applied2, p := planFollow(s, applied, wifiinfo.Info{Current: netA, Next: &netB})
	if s2 != s || *applied2 != netB || len(p.add)+len(p.remove) != 0 {
		t.Fatalf("again: %+v %+v", s2, p)
	}

	// On the new network: the old one is forgotten.
	s3, applied3, p := planFollow(s2, applied2, wifiinfo.Info{Current: netB})
	if s3.Profile != netB.SSID || s3.Next != "" || applied3 != nil || s3.AutoJoin != true {
		t.Fatalf("moved: %+v %v", s3, applied3)
	}
	if len(p.add) != 0 || !reflect.DeepEqual(p.remove, []string{"DIRECT-HS-Q4TZ"}) {
		t.Fatalf("moved plan: %+v", p)
	}
}

// The user called the change off: the network that was to follow is forgotten again.
func TestFollowCalledOff(t *testing.T) {
	set := settings{Profile: netA.SSID, Next: netB.SSID}
	s, applied, p := planFollow(set, ptr(netB), wifiinfo.Info{Current: netA})
	if s.Profile != netA.SSID || s.Next != "" || applied != nil {
		t.Fatalf("%+v %v", s, applied)
	}
	if len(p.add) != 0 || !reflect.DeepEqual(p.remove, []string{netB.SSID}) {
		t.Fatalf("plan %+v", p)
	}
}

// Only the password changes: the profile of the same name is rewritten, and nothing is ever deleted.
func TestFollowPasswordOnly(t *testing.T) {
	set := settings{Profile: netA.SSID}
	s, applied, p := planFollow(set, nil, wifiinfo.Info{Current: netA, Next: &netA2})
	if s.Next != netA.SSID || *applied != netA2 || !reflect.DeepEqual(p.add, []wifiinfo.Network{netA2}) || len(p.remove) != 0 {
		t.Fatalf("announced: %+v %v %+v", s, applied, p)
	}
	// Done: the phone uses the new password.
	done, appliedDone, p := planFollow(s, applied, wifiinfo.Info{Current: netA2})
	if done.Next != "" || appliedDone != nil || len(p.add)+len(p.remove) != 0 {
		t.Fatalf("done: %+v %v %+v", done, appliedDone, p)
	}
	// Called off instead: the profile gets the old password back.
	off, appliedOff, p := planFollow(s, applied, wifiinfo.Info{Current: netA})
	if off.Next != "" || appliedOff != nil || !reflect.DeepEqual(p.add, []wifiinfo.Network{netA}) || len(p.remove) != 0 {
		t.Fatalf("off: %+v %v %+v", off, appliedOff, p)
	}
}

// The computer was off during the change and was put on the new network by hand.
func TestFollowMissedChange(t *testing.T) {
	set := settings{Profile: netA.SSID}
	s, applied, p := planFollow(set, nil, wifiinfo.Info{Current: netB})
	if s.Profile != netB.SSID || s.Next != "" || applied != nil {
		t.Fatalf("%+v %v", s, applied)
	}
	if len(p.add) != 0 || !reflect.DeepEqual(p.remove, []string{netA.SSID}) {
		t.Fatalf("plan %+v", p)
	}
}

func TestWlanProfileXML(t *testing.T) {
	n := wifiinfo.Network{SSID: "DIRECT-HS-a&b", Password: `p<a>s"s'w&rd`}
	text := wlanProfileXML(n, "manual")
	var doc struct {
		Name string `xml:"name"`
		SSID struct {
			Hex  string `xml:"hex"`
			Name string `xml:"name"`
		} `xml:"SSIDConfig>SSID"`
		Mode string `xml:"connectionMode"`
		Key  string `xml:"MSM>security>sharedKey>keyMaterial"`
	}
	if err := xml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Name != n.SSID || doc.SSID.Name != n.SSID || doc.SSID.Hex != "4449524543542D48532D612662" || doc.Mode != "manual" || doc.Key != n.Password {
		t.Fatalf("%+v", doc)
	}
	if profileMode(text) != "manual" || !strings.Contains(wlanProfileXML(n, ""), "<connectionMode>auto</connectionMode>") {
		t.Fatal("mode")
	}
	if profileMode("<WLANProfile></WLANProfile>") != "" {
		t.Fatal("mode of a profile without one")
	}
}

func TestProfileListed(t *testing.T) {
	out := "Profiles on interface Wi-Fi:\r\n\r\nGroup policy profiles (read only)\r\n---------------------------------\r\n    <None>\r\n\r\n" +
		"User profiles\r\n-------------\r\n    All User Profile     : DIRECT-HS-Salon\r\n    All User Profile     : DIRECT-HS-K7PM\r\n" +
		"    Tüm Kullanıcı Profili : EvWiFi\r\n"
	for _, name := range []string{"DIRECT-HS-Salon", "DIRECT-HS-K7PM", "EvWiFi"} {
		if !profileListed(out, name) {
			t.Errorf("%s not found", name)
		}
	}
	for _, name := range []string{"DIRECT-HS-A", "DIRECT-HS-Q4TZ", "Wi-Fi"} {
		if profileListed(out, name) {
			t.Errorf("%s found", name)
		}
	}
}

// What wevtutil prints in a Turkish console: the translated fields are not UTF-8, the rest is.
func TestParseDisconnect(t *testing.T) {
	out := []byte("<Event xmlns='http://schemas.microsoft.com/win/2004/08/events/event'><System><EventID>8003</EventID>" +
		"<TimeCreated SystemTime='2026-10-31T21:00:18.1234567Z'/></System><EventData>" +
		"<Data Name='ConnectionMode'>Profille otomatik ba\xa7lant\x8d</Data><Data Name='ProfileName'>DIRECT-HS-Ev</Data>" +
		"<Data Name='SSID'>DIRECT-HS-Ev</Data><Data Name='Reason'>Ge\x87ici ba\xa7lant\x8d kesme</Data><Data Name='ReasonCode'>13</Data></EventData></Event>")
	d, ok := parseDisconnect(out)
	if !ok || d.Profile != "DIRECT-HS-Ev" || d.Reason != reasonWindowsMovedOn || d.At.Hour() != 21 || d.At.Second() != 18 {
		t.Fatalf("%+v %v", d, ok)
	}
	if _, ok := parseDisconnect([]byte("No events were found.")); ok {
		t.Fatal("parsed nothing")
	}
}

func TestMatchPhoneEitherName(t *testing.T) {
	nets := []netEntry{{Alias: "Wi-Fi", Profile: "DIRECT-HS-Ev", Gateway: "192.168.49.1"}}
	if c, _ := matchPhone(nets, "192.168.49.1", "DIRECT-HS-Q4TZ"); c {
		t.Fatal("matched the old name")
	}
	if c, _ := matchPhone(nets, "192.168.49.1", "DIRECT-HS-Q4TZ", "DIRECT-HS-Ev"); !c {
		t.Fatal("missed the new name")
	}
	if c, _ := matchPhone(nets, "192.168.49.1", "", ""); !c {
		t.Fatal("no names: any network of the phone must do")
	}
	if c, seen := matchPhone(nets, "192.168.49.9", "", ""); c || seen {
		t.Fatalf("no names, other gateway: %v %v", c, seen)
	}
	if s, ok := phoneSSID([]string{"EvWiFi", "DIRECT-HS-Ev"}, "DIRECT-HS-Q4TZ", "DIRECT-HS-Ev"); !ok || s != "DIRECT-HS-Ev" {
		t.Fatal("phoneSSID")
	}
}
