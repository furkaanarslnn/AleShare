package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The project's releases on GitHub. Every release there carries the two files a user needs (the APK and this
// program) and "update.json", which describes it; /releases/latest/download/ always leads to the newest release's
// file. The programs look there now and then and tell the user when a newer release is out.
const (
	releaseRepo = "furkaanarslnn/AleShare"
	onlineEvery = 3 * time.Hour    // between two looks that got an answer
	onlineRetry = 30 * time.Minute // after a look without one: away from the phone there is often no internet
	maxDownload = int64(256 << 20) // no release file is anywhere near this
)

// Where the newest release is described, and where every release's files must lie (variables for the tests).
var (
	manifestURL  = "https://github.com/" + releaseRepo + "/releases/latest/download/update.json"
	downloadBase = "https://github.com/" + releaseRepo + "/releases/download/"
)

// onlineRelease is update.json. Name is what the release is called. In the file that is "label"; its "name" is
// there for the apps of release 1.1, which accept only the name their own rule gives the number (see
// release.ManifestName), and stands in when a file has no label.
type onlineRelease struct {
	Code  int        `json:"code"`
	Name  string     `json:"name"`
	Label string     `json:"label,omitempty"`
	APK   onlineFile `json:"apk"`
	PC    onlineFile `json:"pc"`
}

type onlineFile struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// parseManifest reads update.json. One that names files anywhere but among this project's releases is refused:
// whoever could change the manifest must not be able to send the programs elsewhere for their files.
func parseManifest(data []byte) (onlineRelease, error) {
	var r onlineRelease
	if err := json.Unmarshal(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")), &r); err != nil {
		return r, fmt.Errorf("sürüm bilgisi okunamadı: %w", err)
	}
	if r.Code <= 0 || r.Name == "" {
		return r, errors.New("sürüm bilgisi bozuk")
	}
	if r.Label != "" {
		r.Name, r.Label = r.Label, ""
	}
	for _, f := range []onlineFile{r.APK, r.PC} {
		if !strings.HasPrefix(f.URL, downloadBase) || f.Size <= 0 || f.Size > maxDownload || len(f.SHA256) != 64 {
			return r, errors.New("sürüm bilgisi bozuk")
		}
	}
	return r, nil
}

// onlineState is what this user's programs (the window, the icon next to the clock) know of the releases on GitHub.
type onlineState struct {
	Checked time.Time      `json:"checked"`           // the last look that got an answer
	Tried   time.Time      `json:"tried"`             // the last look at all
	Release *onlineRelease `json:"release,omitempty"` // the newest release, as last seen
	Told    int            `json:"told,omitempty"`    // the newest release the icon has told the user about
}

// due reports whether it is time to look again.
func (s onlineState) due(now time.Time) bool {
	if now.Before(s.Tried) || now.Before(s.Checked) {
		return true // the clock was set back
	}
	return now.Sub(s.Checked) >= onlineEvery && now.Sub(s.Tried) >= onlineRetry
}

// offer is the release worth installing over own (a release number): the newest one known, when it is newer. A
// build that is no release (own 0) is offered nothing.
func (s onlineState) offer(own int) *onlineRelease {
	if s.Release != nil && own > 0 && s.Release.Code > own {
		return s.Release
	}
	return nil
}

// announce is the release to tell the user about now: an offer not told yet.
func (s onlineState) announce(own int) *onlineRelease {
	if r := s.offer(own); r != nil && r.Code > s.Told {
		return r
	}
	return nil
}

// lookResult says what a look at GitHub that began at began came to, for a program of the release own: no answer
// came (answered is false), a newer release is out, or this one is the newest.
func lookResult(s onlineState, began time.Time, own int) (text string, answered bool) {
	switch rel := s.offer(own); {
	case s.Checked.Before(began):
		return "GitHub'a ulaşılamadı. İnternet bağlantına bak.", false
	case rel != nil:
		return "AleShare " + rel.Name + " yayımlandı.", true
	default:
		return "AleShare güncel.", true
	}
}
