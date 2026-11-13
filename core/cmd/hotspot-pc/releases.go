package main

import (
	"regexp"
	"strconv"
	"time"

	"hotspot/core/internal/release"
)

// Releases move through the phone (see wgcore/update.go): this computer brings the phone what it has newer (its own
// signed program, and the APK that came with it) and takes what the phone has newer (a newer program, which it
// installs once its release signature checks out, and the APK, kept to give to a phone that is behind).

// releaseState is what this computer knows of the releases, kept in releases.json.
type releaseState struct {
	APK      int `json:"apk"`       // the release of the APK kept in Hotspot.apk (0: none)
	PhoneApp int `json:"phone_app"` // the newest app the phone said it has, last time it was asked
	// PhoneAppName is what the phone called that release. A release that got its name after this program was
	// built has one this program cannot work out from the number.
	PhoneAppName string `json:"phone_app_name,omitempty"`
	PhonePC      int    `json:"phone_pc"` // the newest Windows program it said it has
	// Tried holds when a transfer was last tried ("bring-apk-12"…), so that one the phone turns away is not tried
	// again every minute.
	Tried map[string]time.Time `json:"tried,omitempty"`
}

// retryAfter is how long a failed transfer of one release waits before it is tried again.
const retryAfter = 30 * time.Minute

// syncPlan is what to move, one way or the other.
type syncPlan struct {
	takeProgram, bringProgram, takeAPK, bringAPK bool
}

// planSync compares this computer's program (own) and kept APK with what the phone has. A release the phone does
// not know is 0 (an older phone app says nothing).
func planSync(own, kept, phoneApp, phonePC int) syncPlan {
	return syncPlan{
		takeProgram:  phonePC > own && own > 0,
		bringProgram: own > phonePC,
		takeAPK:      phoneApp > kept,
		bringAPK:     kept > phoneApp && phoneApp > 0,
	}
}

// due reports whether a transfer may be tried now, and notes the try.
func (s *releaseState) due(what string, code int, now time.Time) bool {
	key := what + "-" + strconv.Itoa(code)
	if t, ok := s.Tried[key]; ok && now.Sub(t) < retryAfter {
		return false
	}
	if s.Tried == nil {
		s.Tried = map[string]time.Time{}
	}
	for k, t := range s.Tried { // keep it small
		if now.Sub(t) > 24*time.Hour {
			delete(s.Tried, k)
		}
	}
	s.Tried[key] = now
	return true
}

var apkFile = regexp.MustCompile(`^(?:AleShare|Hotspot)-(\d+(?:\.\d+)+)\.apk$`)

// publishedAPK is the name the published APK always has, whatever its release, and what a browser makes of it when
// the file is downloaded a second time ("AleShare (1).apk"). Which release such a file is stands inside it
// (release.APKCode).
var publishedAPK = regexp.MustCompile(`^AleShare( \(\d+\))?\.apk$`)

// apkRelease reads the release from the name that builds up to the first published one gave an APK
// ("AleShare-1.1.apk", "AleShare-1.0.32.apk", "Hotspot-1.0.12.apk" before 1.0.32); 0 when it is not one.
func apkRelease(name string) int {
	m := apkFile.FindStringSubmatch(name)
	if m == nil {
		return 0
	}
	return release.Code(m[1])
}

// versionCode is the release number of a release's name, "1.1" or "1.0.12" (0 for a build that is not a release).
func versionCode(v string) int { return release.Code(v) }
