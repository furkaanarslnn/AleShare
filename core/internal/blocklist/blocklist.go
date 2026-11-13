// Package blocklist decides which DNS names the sharing phone answers with nothing.
//
// Ads and trackers: the names of StevenBlack/hosts (the unified list of ad and malware hosts, MIT license, as of
// 29 October 2026), bundled as ads.txt.gz; gen.go makes it from a newer hosts file. A name is blocked when it is on
// the list itself, as hosts files work.
//
// Saving mobile data: the servers that big automatic downloads come from (operating system and game updates, store
// downloads), blocked with every name under them. Nothing else on those vendors' sites is touched.
package blocklist

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"strings"
	"sync"
)

//go:embed ads.txt.gz
var adsGz []byte

var (
	adsOnce sync.Once
	ads     map[string]struct{}
)

func loadAds() {
	ads = map[string]struct{}{}
	zr, err := gzip.NewReader(bytes.NewReader(adsGz))
	if err != nil {
		return
	}
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		if name := sc.Text(); name != "" {
			ads[name] = struct{}{}
		}
	}
}

// Preload reads the ad list (a fraction of a second, a few MB), so that the first query does not wait for it.
func Preload() { adsOnce.Do(loadAds) }

// AdCount is how many names the ad list holds.
func AdCount() int {
	Preload()
	return len(ads)
}

// updateServers are where big automatic downloads come from. Each blocks itself and every name under it.
var updateServers = []string{
	// Windows Update, Delivery Optimization and Microsoft Store downloads.
	"windowsupdate.com", "update.microsoft.com", "delivery.mp.microsoft.com", "do.dsp.mp.microsoft.com", "emdl.ws.microsoft.com",
	// Game downloads: Steam, Epic Games, Battle.net.
	"steamcontent.com", "download.epicgames.com", "epicgames-download1.akamaized.net", "fastly-download.epicgames.com",
	"level3.blizzard.com", "blzddist1-a.akamaihd.net",
	// Apple software updates.
	"swcdn.apple.com", "swdist.apple.com", "updates.cdn-apple.com", "mesu.apple.com",
}

// normal makes a name as it comes in a query comparable: lower case, without the final dot.
func normal(name string) string { return strings.ToLower(strings.TrimSuffix(name, ".")) }

// IsAd reports whether a name is on the ad and tracker list.
func IsAd(name string) bool {
	Preload()
	_, ok := ads[normal(name)]
	return ok
}

// IsUpdate reports whether a name is a server of big automatic downloads, or under one.
func IsUpdate(name string) bool {
	n := normal(name)
	for _, s := range updateServers {
		if n == s || strings.HasSuffix(n, "."+s) {
			return true
		}
	}
	return false
}
