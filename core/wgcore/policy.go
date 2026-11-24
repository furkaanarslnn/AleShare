package wgcore

import (
	"fmt"
	"net/netip"
	"sync"

	"hotspot/core/internal/blocklist"
	"hotspot/core/internal/gateway"
)

// policy is what the user decides about the shared traffic: names nobody gets an address for, the priority of
// calls and games, and devices whose internet is paused. Each switch takes effect at once, running or not.
type policy struct {
	mu       sync.RWMutex
	blockAds bool
	saveData bool
	priority bool
	paused   map[netip.Addr]bool // tunnel addresses of the paused devices
	// The speed limits: per device, under both its tunnel addresses (one limiter for the two), and of everyone.
	limits   map[netip.Addr]*gateway.Limiter
	everyone gateway.Limiter
}

// SetSpeedLimit holds a paired device to kbps kilobits per second each way, its flows together; 0 lifts the limit.
// It takes effect at once, for the flows under way too.
func (c *Core) SetSpeedLimit(name string, kbps int64) error {
	addrs := c.addrsOf(name)
	if len(addrs) == 0 {
		return fmt.Errorf("wgcore: no device named %q", name)
	}
	c.pol.mu.Lock()
	defer c.pol.mu.Unlock()
	if c.pol.limits == nil {
		c.pol.limits = map[netip.Addr]*gateway.Limiter{}
	}
	l := c.pol.limits[addrs[0]]
	if l == nil {
		l = new(gateway.Limiter)
	}
	l.SetRate(bytesPerSecond(kbps))
	for _, a := range addrs {
		if kbps > 0 {
			c.pol.limits[a] = l
		} else {
			delete(c.pol.limits, a)
		}
	}
	return nil
}

// SetSpeedCap holds all the shared traffic together to kbps kilobits per second each way (a phone that got hot
// slows down); 0 lifts it.
func (c *Core) SetSpeedCap(kbps int64) { c.pol.everyone.SetRate(bytesPerSecond(kbps)) }

func bytesPerSecond(kbps int64) float64 { return float64(max(kbps, 0)) * 1000 / 8 }

// addrsOf returns the tunnel addresses of the device called name.
func (c *Core) addrsOf(name string) []netip.Addr {
	c.mu.Lock()
	defer c.mu.Unlock()
	var addrs []netip.Addr
	for _, p := range c.state.Peers {
		if p.Name == name {
			for _, a := range []netip.Addr{p.IPv4, p.IPv6} {
				if a.IsValid() {
					addrs = append(addrs, a)
				}
			}
		}
	}
	return addrs
}

func (c *Core) limitsOf(client netip.Addr) (device, everyone *gateway.Limiter) {
	c.pol.mu.RLock()
	device = c.pol.limits[client]
	c.pol.mu.RUnlock()
	return device, &c.pol.everyone
}

// SetBlockAds switches the blocking of ad and tracker names for every device (see internal/blocklist).
func (c *Core) SetBlockAds(on bool) {
	if on {
		go blocklist.Preload()
	}
	c.pol.mu.Lock()
	c.pol.blockAds = on
	c.pol.mu.Unlock()
}

// SetSaveData switches the blocking of the servers big automatic downloads come from (system and game updates).
func (c *Core) SetSaveData(on bool) {
	c.pol.mu.Lock()
	c.pol.saveData = on
	c.pol.mu.Unlock()
}

// SetPriority switches the priority of calls and games over big transfers (see gateway.Options.Priority).
func (c *Core) SetPriority(on bool) {
	c.pol.mu.Lock()
	c.pol.priority = on
	c.pol.mu.Unlock()
}

// SetPaused pauses or resumes the internet of a paired device: paused, its flows end at once and new ones are
// refused, while it keeps its tunnel (so it comes back the moment it is resumed).
func (c *Core) SetPaused(name string, paused bool) error {
	addrs := c.addrsOf(name)
	if len(addrs) == 0 {
		return fmt.Errorf("wgcore: no device named %q", name)
	}
	c.pol.mu.Lock()
	if c.pol.paused == nil {
		c.pol.paused = map[netip.Addr]bool{}
	}
	for _, a := range addrs {
		if paused {
			c.pol.paused[a] = true
		} else {
			delete(c.pol.paused, a)
		}
	}
	c.pol.mu.Unlock()
	c.mu.Lock()
	srv := c.srv
	c.mu.Unlock()
	if paused && srv != nil {
		for _, a := range addrs {
			srv.CutClient(a)
		}
	}
	return nil
}

func (c *Core) blockName(name string) bool {
	c.pol.mu.RLock()
	ads, saver := c.pol.blockAds, c.pol.saveData
	c.pol.mu.RUnlock()
	return (ads && blocklist.IsAd(name)) || (saver && blocklist.IsUpdate(name))
}

func (c *Core) isPaused(client netip.Addr) bool {
	c.pol.mu.RLock()
	defer c.pol.mu.RUnlock()
	return c.pol.paused[client]
}

func (c *Core) priorityOn() bool {
	c.pol.mu.RLock()
	defer c.pol.mu.RUnlock()
	return c.pol.priority
}
