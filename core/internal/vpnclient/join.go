package vpnclient

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// A join link is what a Hotspot phone shows as one QR code for another phone or tablet running this app: the
// Wi-Fi network to enter and the tunnel to open, so one scan sets up both.
//
//	hotspot://join?d=<base64url of {"v":1,"s":ssid,"p":password,"c":config}>
const (
	joinPrefix  = "hotspot://join?d="
	joinVersion = 1
	maxJoinSize = 8 << 10
)

// Join is a parsed join link.
type Join struct {
	SSID     string
	Password string
	Config   *Config
	// Text is the WireGuard configuration as it came, for keeping.
	Text string
}

type joinJSON struct {
	V int    `json:"v"`
	S string `json:"s"`
	P string `json:"p"`
	C string `json:"c"`
}

// EncodeJoin makes a join link, after checking that the parts would work.
func EncodeJoin(ssid, password, config string) (string, error) {
	if _, err := checkJoin(joinJSON{V: joinVersion, S: ssid, P: password, C: config}); err != nil {
		return "", err
	}
	raw, err := json.Marshal(joinJSON{V: joinVersion, S: ssid, P: password, C: config})
	if err != nil {
		return "", err
	}
	return joinPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// ParseJoin reads a join link, as scanned or pasted. Spaces and line breaks around it are ignored.
func ParseJoin(text string) (*Join, error) {
	text = strings.TrimSpace(text)
	if len(text) > maxJoinSize {
		return nil, errors.New("vpnclient: the link is too long")
	}
	rest, ok := strings.CutPrefix(text, joinPrefix)
	if !ok {
		return nil, errors.New("vpnclient: this is not a Hotspot link")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(rest, "="))
	if err != nil {
		return nil, fmt.Errorf("vpnclient: the link is damaged: %w", err)
	}
	var j joinJSON
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, fmt.Errorf("vpnclient: the link is damaged: %w", err)
	}
	return checkJoin(j)
}

func checkJoin(j joinJSON) (*Join, error) {
	switch {
	case j.V != joinVersion:
		return nil, fmt.Errorf("vpnclient: link version %d is not supported", j.V)
	case j.S == "" || len(j.S) > 32:
		return nil, errors.New("vpnclient: the Wi-Fi name must be 1 to 32 bytes")
	case len(j.P) < 8 || len(j.P) > 63:
		return nil, errors.New("vpnclient: the Wi-Fi password must be 8 to 63 characters")
	case !printable(j.P):
		// What a WPA2 password may hold. Android refuses anything else outright, so a link that carried it (made
		// by hand: the app never makes one) would end the app of whoever saved it, at every start.
		return nil, errors.New("vpnclient: the Wi-Fi password must be plain ASCII characters")
	}
	c, err := Parse(j.C)
	if err != nil {
		return nil, err
	}
	return &Join{SSID: j.S, Password: j.P, Config: c, Text: j.C}, nil
}

// printable reports whether s holds only the printable ASCII characters, space to tilde.
func printable(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < ' ' || s[i] > '~' {
			return false
		}
	}
	return true
}
