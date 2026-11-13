package wgcore

import (
	"net"
	"net/http"
	"strings"
	"testing"
)

// The splitting is off until someone turns it on, and the switch holds without the gateway running.
func TestSplitTLSSwitch(t *testing.T) {
	c, err := NewCore("")
	if err != nil {
		t.Fatal(err)
	}
	if c.SplitTLS() {
		t.Fatal("on by default")
	}
	c.SetSplitTLS(true)
	if !c.SplitTLS() {
		t.Fatal("did not turn on")
	}
	c.SetSplitTLS(false)
	if c.SplitTLS() || c.Stats().TCPSplit != 0 {
		t.Fatal("did not turn off")
	}
}

// A device asks with the configuration it was given; the phone answers, and counts who has learned a change.
func TestAskWifi(t *testing.T) {
	c, err := NewCore("")
	if err != nil {
		t.Fatal(err)
	}
	config, err := c.AddPeer("Tab", "127.0.0.1:51820")
	if err != nil {
		t.Fatal(err)
	}
	// The real listener only answers on the Wi-Fi Direct network; this one stands in for it.
	conn, err := net.ListenPacket("udp4", "127.0.0.1:51822")
	if err != nil {
		t.Skipf("port 51822 is taken: %v", err)
	}
	defer conn.Close()
	go c.wifi.Serve(conn, nil)

	c.SetWifi("DIRECT-HS-Q4TZ", "abcdEFGH2345jkmn", "", "")
	w, err := AskWifi(config, nil)
	if err != nil {
		t.Fatal(err)
	}
	if w.SSID != "DIRECT-HS-Q4TZ" || w.Password != "abcdEFGH2345jkmn" || w.NextSSID != "" {
		t.Fatalf("got %+v", w)
	}

	c.SetWifi("DIRECT-HS-Q4TZ", "abcdEFGH2345jkmn", "DIRECT-HS-Ev", "yeni-parola-123")
	if c.WifiLearned() != "" {
		t.Fatal("learned before asking")
	}
	if w, err = AskWifi(config, nil); err != nil || w.NextSSID != "DIRECT-HS-Ev" || w.NextPassword != "yeni-parola-123" {
		t.Fatalf("got %+v, %v", w, err)
	}
	if got := c.WifiLearned(); got != "Tab" {
		t.Fatalf("learned %q", got)
	}
	if got := c.WifiAskers(); got != "Tab" {
		t.Fatalf("askers %q", got)
	}

	// While sharing runs (its store is served), the answer names the app and where to get it.
	if w.AppCode != 0 || w.UpdateURL != "" {
		t.Fatalf("an app offered before any was: %+v", w)
	}
	c.SetAppUpdate("base.apk", 9, "1.0.9")
	c.mu.Lock()
	c.updateSrv = &http.Server{} // stands in for the running store
	c.publishLocked()
	c.mu.Unlock()
	_, _, token := c.upd.offers()
	if w, err = AskWifi(config, nil); err != nil || w.AppCode != 9 || w.AppName != "1.0.9" ||
		w.UpdateURL != "http://127.0.0.1:51823/hotspot.apk?t="+token || w.SSID != "DIRECT-HS-Q4TZ" {
		t.Fatalf("got %+v, %v", w, err)
	}
	// What is on offer is told at once when it changes.
	c.SetAppUpdate("received.apk", 10, "1.0.10")
	if w, err = AskWifi(config, nil); err != nil || w.AppCode != 10 {
		t.Fatalf("after a newer app came: %+v, %v", w, err)
	}
	c.mu.Lock()
	c.updateSrv = nil
	c.mu.Unlock()

	// A removed device is told nothing.
	c.RemovePeer("Tab")
	if _, err := AskWifi(config, nil); err == nil || !strings.Contains(err.Error(), "no answer") {
		t.Fatalf("removed device: %v", err)
	}
}
