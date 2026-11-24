package wgcore

import (
	"net/netip"
	"testing"
)

// The switches decide what is blocked, each at once and on its own.
func TestBlockSwitches(t *testing.T) {
	c, _ := NewCore("")
	for _, name := range []string{"doubleclick.net.", "cache1.steamcontent.com.", "wikipedia.org."} {
		if c.blockName(name) {
			t.Fatalf("%s blocked with every switch off", name)
		}
	}
	c.SetBlockAds(true)
	if !c.blockName("doubleclick.net.") || c.blockName("cache1.steamcontent.com.") {
		t.Fatal("ad blocking blocks the wrong names")
	}
	c.SetSaveData(true)
	c.SetBlockAds(false)
	if c.blockName("doubleclick.net.") || !c.blockName("cache1.steamcontent.com.") || c.blockName("wikipedia.org.") {
		t.Fatal("data saving blocks the wrong names")
	}
	if c.priorityOn() {
		t.Fatal("priority on by default")
	}
	c.SetPriority(true)
	if !c.priorityOn() {
		t.Fatal("priority did not switch on")
	}
}

// Pausing goes by the device's name and covers both of its tunnel addresses; other devices are untouched.
func TestPauseByName(t *testing.T) {
	c, _ := NewCore("")
	c.AddPeer("Tab", "192.168.49.1:51820")
	c.AddPeer("Laptop", "192.168.49.1:51820")
	var tab, laptop []netip.Addr
	for _, p := range c.state.Peers {
		if p.Name == "Tab" {
			tab = []netip.Addr{p.IPv4, p.IPv6}
		} else {
			laptop = []netip.Addr{p.IPv4, p.IPv6}
		}
	}
	if err := c.SetPaused("Tab", true); err != nil {
		t.Fatal(err)
	}
	if !c.isPaused(tab[0]) || !c.isPaused(tab[1]) || c.isPaused(laptop[0]) {
		t.Fatal("the wrong addresses are paused")
	}
	c.SetPaused("Tab", false)
	if c.isPaused(tab[0]) {
		t.Fatal("still paused after resuming")
	}
	if err := c.SetPaused("Nobody", true); err == nil {
		t.Fatal("paused a device that does not exist")
	}
}

func TestSpeedLimitByName(t *testing.T) {
	c, _ := NewCore("")
	c.AddPeer("Tab", "192.168.49.1:51820")
	c.AddPeer("Laptop", "192.168.49.1:51820")
	tab, laptop := c.addrsOf("Tab"), c.addrsOf("Laptop")
	if err := c.SetSpeedLimit("Tab", 5000); err != nil {
		t.Fatal(err)
	}
	d4, everyone := c.limitsOf(tab[0])
	d6, _ := c.limitsOf(tab[1])
	if d4 == nil || d4 != d6 || d4.Rate() != 625000 {
		t.Fatalf("5 Mbit/s for both addresses of one device, got %v and %v", d4, d6)
	}
	if d, _ := c.limitsOf(laptop[0]); d != nil {
		t.Fatal("a device without a limit got one")
	}
	if everyone == nil || everyone.Rate() != 0 {
		t.Fatal("nobody asked for a limit of everyone")
	}
	c.SetSpeedCap(10000)
	if _, e := c.limitsOf(laptop[0]); e.Rate() != 1250000 {
		t.Fatalf("everyone's limit is %v bytes/s", e.Rate())
	}
	c.SetSpeedLimit("Tab", 0)
	if d, _ := c.limitsOf(tab[0]); d != nil {
		t.Fatal("still limited after lifting the limit")
	}
	if err := c.SetSpeedLimit("Nobody", 1000); err == nil {
		t.Fatal("limited a device that does not exist")
	}
}

// A device that is removed takes its pause and its speed limit with it: the next device gets the same tunnel
// address (the lowest free one) and must start clean.
func TestRemovedDeviceLeavesNothingOnItsAddress(t *testing.T) {
	c, _ := NewCore("")
	c.AddPeer("Tab", "192.168.49.1:51820")
	addr := c.state.Peers[0].IPv4
	c.SetPaused("Tab", true)
	c.SetSpeedLimit("Tab", 2000)
	if !c.RemovePeer("Tab") {
		t.Fatal("not removed")
	}
	c.AddPeer("Laptop", "192.168.49.1:51820")
	if c.state.Peers[0].IPv4 != addr {
		t.Fatal("the test expects the address to be handed out again")
	}
	if c.isPaused(addr) {
		t.Fatal("the new device is paused")
	}
	c.pol.mu.RLock()
	_, limited := c.pol.limits[addr]
	c.pol.mu.RUnlock()
	if limited {
		t.Fatal("the new device has the old device's speed limit")
	}
}
