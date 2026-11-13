package main

import (
	"bytes"
	"encoding/json"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// netEntry is one connected network of this computer.
type netEntry struct {
	Alias   string `json:"Alias"`   // interface, e.g. "Wi-Fi"
	Profile string `json:"Profile"` // Windows' name of the network, normally the SSID
	Gateway string `json:"Gateway"` // IPv4 default gateway: the phone, on its own network
}

// parseNetworks reads the JSON that PowerShell printed. ConvertTo-Json prints
// a single object instead of a one-element array, and nothing at all when
// there is no network.
func parseNetworks(data []byte) ([]netEntry, error) {
	data = bytes.TrimSpace(bytes.TrimPrefix(bytes.TrimSpace(data), []byte("\xef\xbb\xbf")))
	if len(data) == 0 {
		return nil, nil
	}
	if data[0] == '[' {
		var out []netEntry
		err := json.Unmarshal(data, &out)
		return out, err
	}
	var one netEntry
	err := json.Unmarshal(data, &one)
	return []netEntry{one}, err
}

// parseSSIDs lists the networks this PC's Wi-Fi is connected to, from the text of
// `netsh wlan show interfaces`. Only the key "SSID" is used, which Windows does not
// translate (the other labels are localized).
func parseSSIDs(out string) []string {
	var ssids []string
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(key) == "SSID" {
			if s := strings.TrimSpace(value); s != "" {
				ssids = append(ssids, s)
			}
		}
	}
	return ssids
}

// parseVisibleSSIDs lists the networks in range, from the text of `netsh wlan show networks`.
// The lines look like "SSID 1 : name"; the "BSSID" lines and the localized ones are ignored.
func parseVisibleSSIDs(out string) []string {
	var ssids []string
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(key)
		if len(fields) == 2 && fields[0] == "SSID" && strings.Trim(fields[1], "0123456789") == "" {
			if s := strings.TrimSpace(value); s != "" {
				ssids = append(ssids, s)
			}
		}
	}
	return ssids
}

// phoneSSID picks the phone's network out of the networks in range; profiles are the names it may have.
func phoneSSID(visible []string, profiles ...string) (string, bool) {
	for _, s := range visible {
		for _, p := range profiles {
			if p != "" && strings.HasPrefix(s, p) {
				return s, true
			}
		}
	}
	return "", false
}

// wlanDisconnect is one "disconnected" event (8003) of Windows' WLAN service.
type wlanDisconnect struct {
	Profile string
	Reason  int // 2: the user disconnected, 3: another connection was asked for, 13: Windows' own temporary disconnect
	At      time.Time
}

// reasonWindowsMovedOn is the disconnect Windows makes by itself when the network has no internet of its own
// (on the phone's network the internet comes through the tunnel) and a known network with internet is in range.
const reasonWindowsMovedOn = 13

var (
	eventTime    = regexp.MustCompile(`<TimeCreated SystemTime='([^']+)'`)
	eventProfile = regexp.MustCompile(`<Data Name='ProfileName'>([^<]*)</Data>`)
	eventReason  = regexp.MustCompile(`<Data Name='ReasonCode'>(\d+)</Data>`)
)

// parseDisconnect reads the event that `wevtutil qe ... /f:xml` printed. Only the fields that Windows does not
// translate are used; the translated ones may not even be valid UTF-8 in the console's code page.
func parseDisconnect(out []byte) (wlanDisconnect, bool) {
	t, p, r := eventTime.FindSubmatch(out), eventProfile.FindSubmatch(out), eventReason.FindSubmatch(out)
	if t == nil || p == nil || r == nil {
		return wlanDisconnect{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, string(t[1]))
	if err != nil {
		return wlanDisconnect{}, false
	}
	reason, err := strconv.Atoi(string(r[1]))
	if err != nil {
		return wlanDisconnect{}, false
	}
	return wlanDisconnect{Profile: html.UnescapeString(string(p[1])), Reason: reason, At: at}, true
}

// health is what the watchdog remembers between two runs of the scheduled task.
type health struct {
	Fails       int       `json:"fails"`        // consecutive probes the phone did not answer
	LastFail    time.Time `json:"last_fail"`    // when the latest of them was
	PausedUntil time.Time `json:"paused_until"` // do not start the tunnel before this
	// NoJoinUntil keeps the automatic joining quiet for a while: after the phone's network turned out
	// to be a dead end (its app died), or a connection attempt failed.
	NoJoinUntil time.Time `json:"no_join_until"`
	DeadEnds    int       `json:"dead_ends"` // gave up this many times in a row; each makes the quiet time longer
	// BroughtBack is when this computer last went back to the phone's network after Windows had left it by itself;
	// once in a while at most, so that Windows and this program never take turns.
	BroughtBack time.Time `json:"brought_back"`
	// Answers is set once the phone has answered the question about its Wi-Fi network (apps since 1.0.4 do, at
	// once, whenever they share). From then on its silence on arrival means its app is down, which takes seconds
	// to tell instead of a tunnel's worth of failed probes.
	Answers bool `json:"answers"`
}

// bringBackEvery is how seldom the computer goes back after Windows left the phone's network by itself.
const bringBackEvery = 10 * time.Minute

// deadEndPause is how long to stay away after the given number of earlier dead ends: a minute, then twice as
// long each time, but never more than two hours. A look costs a few seconds off the usual network (join, ask the
// phone, leave if it is silent), so a phone whose app was only updated or restarted is back within a minute or
// two, and a forgotten dead group costs a few seconds rarely.
func deadEndPause(earlier int) time.Duration {
	d := pauseAfterDead
	for i := 0; i < earlier && d < 2*time.Hour; i++ {
		d *= 2
	}
	if d > 2*time.Hour {
		d = 2 * time.Hour
	}
	return d
}

const (
	failuresToGiveUp = 2 // one missed probe may be a hiccup; two apart are not
	failureSpacing   = 30 * time.Second
	pauseAfterGiveUp = 3 * time.Minute
	pauseAfterDead   = time.Minute     // no automatic joining after a dead end
	pauseAfterFailed = 2 * time.Minute // ... or after a failed attempt
)

// judge records the result of one probe and reports whether the tunnel must be
// given up: the phone's network is there but nobody answers on it, which would
// leave this PC without any internet because the tunnel blocks everything else.
// Network events can start several runs at once; failures closer together than
// failureSpacing are one incident and count once.
func (h *health) judge(answered bool, now time.Time) (giveUp bool) {
	if answered {
		h.Fails = 0
		h.DeadEnds = 0
		return false
	}
	if h.Fails > 0 && now.Sub(h.LastFail) < failureSpacing {
		return false
	}
	h.Fails++
	h.LastFail = now
	if h.Fails < failuresToGiveUp {
		return false
	}
	h.Fails = 0
	h.PausedUntil = now.Add(pauseAfterGiveUp)
	h.NoJoinUntil = now.Add(deadEndPause(h.DeadEnds))
	h.DeadEnds++
	return true
}

func (h health) paused(now time.Time) bool { return now.Before(h.PausedUntil) }

// arrival is what to do on the phone's network before the tunnel opens.
type arrival int

const (
	openTunnel arrival = iota
	waitPaused         // the phone stopped answering a moment ago; the watchdog's pause still holds
	stillDead          // a phone that answers when it shares is silent after it was given up on: leave again
)

// arrive decides on arrival at the phone's network, from whether the phone answered the question about its
// network just now. An answer is proof of life: every pause is over. Silence is a verdict only for a phone that
// has answered before and was given up on since; otherwise the tunnel opens and the watchdog judges as ever.
func (h *health) arrive(answered bool, now time.Time) arrival {
	if answered {
		h.Answers = true
		h.Fails, h.DeadEnds = 0, 0
		h.PausedUntil, h.NoJoinUntil = time.Time{}, time.Time{}
		return openTunnel
	}
	if h.Answers && h.DeadEnds > 0 {
		h.NoJoinUntil = now.Add(deadEndPause(h.DeadEnds))
		h.DeadEnds++
		return stillDead
	}
	if h.paused(now) {
		return waitPaused
	}
	return openTunnel
}

func (h health) joinQuiet(now time.Time) bool { return now.Before(h.NoJoinUntil) }

// matchPhone reports whether the phone's network is connected, and whether a
// network of the paired name is connected at all (its address may still be
// pending, and the caller then waits for it). profiles are the names the
// network may have; none (or only empty ones) means any name.
func matchPhone(nets []netEntry, phone string, profiles ...string) (connected, profileSeen bool) {
	named := false
	for _, p := range profiles {
		named = named || p != ""
	}
	for _, n := range nets {
		profileOK := !named
		for _, p := range profiles {
			profileOK = profileOK || (p != "" && strings.HasPrefix(n.Profile, p))
		}
		if profileOK && n.Gateway == phone {
			return true, true
		}
		if named && profileOK {
			profileSeen = true
		}
	}
	return false, profileSeen
}
