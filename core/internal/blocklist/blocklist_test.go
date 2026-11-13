package blocklist

import "testing"

func TestAds(t *testing.T) {
	if n := AdCount(); n < 50000 {
		t.Fatalf("the list holds %d names", n)
	}
	for _, name := range []string{"doubleclick.net.", "DoubleClick.NET", "ad.doubleclick.net"} {
		if !IsAd(name) {
			t.Errorf("%s not blocked", name)
		}
	}
	for _, name := range []string{"google.com.", "youtube.com", "wikipedia.org", "whatsapp.net", "hotspot.local"} {
		if IsAd(name) {
			t.Errorf("%s blocked", name)
		}
	}
}

func TestUpdates(t *testing.T) {
	for _, name := range []string{"windowsupdate.com.", "7.tlu.dl.delivery.mp.microsoft.com", "cache8.steamcontent.com", "swcdn.apple.com"} {
		if !IsUpdate(name) {
			t.Errorf("%s not blocked", name)
		}
	}
	for _, name := range []string{"microsoft.com", "login.microsoftonline.com", "store.steampowered.com", "apple.com", "notwindowsupdate.com"} {
		if IsUpdate(name) {
			t.Errorf("%s blocked", name)
		}
	}
}
