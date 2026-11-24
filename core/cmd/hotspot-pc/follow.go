package main

import (
	"encoding/hex"
	"html"
	"strings"

	"hotspot/core/internal/wifiinfo"
)

// followPlan is what makes this computer follow the phone's Wi-Fi network: the Windows Wi-Fi profiles to add
// (each with its password; one of the same name is replaced) and those to delete, in that order.
type followPlan struct {
	add    []wifiinfo.Network
	remove []string
}

// planFollow compares what the phone said with what this computer knows. applied is the network that follows,
// as a Windows profile was last made for it (nil: none). It returns the new settings and applied network, and
// the profiles to change.
//
// While the user changes the network on the phone, the phone tells both networks; this computer then knows
// both (settings.Profile and settings.Next) and is at home on whichever is there. Once the phone opens the new
// one and this computer is on it, the old one is forgotten.
func planFollow(set settings, applied *wifiinfo.Network, info wifiinfo.Info) (settings, *wifiinfo.Network, followPlan) {
	var p followPlan
	cur := info.Current
	switch {
	case cur.SSID != set.Profile:
		// The phone moved on to the network that followed, or to one this computer missed while it was off.
		old := set.Profile
		set.Profile = cur.SSID
		if set.Next == cur.SSID {
			set.Next, applied = "", nil
		}
		if old != "" && (info.Next == nil || info.Next.SSID != old) {
			p.remove = append(p.remove, old)
		}
	case set.Next == cur.SSID && info.Next == nil:
		// Only the password was to change, and that is done or was called off: the profile of this name must hold
		// the password the phone uses now.
		if applied != nil && *applied != cur {
			p.add = append(p.add, cur)
		}
		set.Next, applied = "", nil
	}
	switch {
	case info.Next != nil:
		if applied == nil || *applied != *info.Next {
			p.add = append(p.add, *info.Next)
			n := *info.Next
			applied = &n
		}
		set.Next = info.Next.SSID
	case set.Next != "":
		// Called off: the network that was to follow is not coming.
		if set.Next != set.Profile {
			p.remove = append(p.remove, set.Next)
		}
		set.Next, applied = "", nil
	}
	return set, applied, p
}

// knownNetwork reports whether ssid is the phone's network, as it is now or as it will be after a change.
// Windows may add " 2" to the name of a network it has seen before, hence the prefix.
func knownNetwork(set settings, ssid string) bool {
	for _, p := range []string{set.Profile, set.Next} {
		if p != "" && strings.HasPrefix(ssid, p) {
			return true
		}
	}
	return false
}

// wlanProfileXML is a Windows Wi-Fi profile for a network of the phone. mode is "auto" (Windows joins it by
// itself when it is in range) or "manual".
func wlanProfileXML(n wifiinfo.Network, mode string) string {
	if mode != "manual" {
		mode = "auto"
	}
	name := html.EscapeString(n.SSID)
	return `<?xml version="1.0"?>
<WLANProfile xmlns="http://www.microsoft.com/networking/WLAN/profile/v1">
	<name>` + name + `</name>
	<SSIDConfig>
		<SSID>
			<hex>` + strings.ToUpper(hex.EncodeToString([]byte(n.SSID))) + `</hex>
			<name>` + name + `</name>
		</SSID>
	</SSIDConfig>
	<connectionType>ESS</connectionType>
	<connectionMode>` + mode + `</connectionMode>
	<MSM>
		<security>
			<authEncryption>
				<authentication>WPA2PSK</authentication>
				<encryption>AES</encryption>
				<useOneX>false</useOneX>
			</authEncryption>
			<sharedKey>
				<keyType>passPhrase</keyType>
				<protected>false</protected>
				<keyMaterial>` + html.EscapeString(n.Password) + `</keyMaterial>
			</sharedKey>
		</security>
	</MSM>
</WLANProfile>
`
}

// profileListed reports whether `netsh wlan show profiles` lists a profile of this name. Only the names are
// compared (the labels in front of them are translated); netsh answers "deleted" and "not found" alike with
// success, so a deletion is checked this way.
func profileListed(out, name string) bool {
	for _, line := range strings.Split(out, "\n") {
		if _, value, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(value) == name {
			return true
		}
	}
	return false
}

// profileMode reads the connection mode out of an exported Windows Wi-Fi profile; "" if it says none.
func profileMode(xml string) string {
	_, rest, ok := strings.Cut(xml, "<connectionMode>")
	if !ok {
		return ""
	}
	mode, _, _ := strings.Cut(rest, "</connectionMode>")
	return strings.TrimSpace(mode)
}
