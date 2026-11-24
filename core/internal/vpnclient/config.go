// Package vpnclient is the other end of the tunnel: a device (a phone or tablet) that reaches the internet
// through a Hotspot phone. It reads the WireGuard configuration the phone made for it, brings the tunnel up
// on a TUN device it is given, and reports whether the tunnel really works.
package vpnclient

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"hotspot/core/internal/wgkey"
)

const (
	minMTU     = 576
	maxMTU     = 1500
	defaultMTU = 1280
	// keepalive keeps the tunnel's state fresh, so a vanished phone is noticed within half a minute.
	keepalive = 25
)

// Config is what the tunnel needs from a WireGuard configuration file.
type Config struct {
	PrivateKey wgkey.Key
	Addresses  []netip.Prefix // this device's addresses inside the tunnel
	DNS        []netip.Addr
	MTU        int

	PublicKey    wgkey.Key // the phone's key
	PresharedKey wgkey.Key // zero when there is none
	Endpoint     string    // host:port of the phone
	AllowedIPs   []netip.Prefix
	Keepalive    int // seconds
}

// Parse reads the .conf format of wg-quick. Only what a Hotspot phone writes is understood, and exactly one peer.
func Parse(text string) (*Config, error) {
	var (
		c       = &Config{MTU: defaultMTU, Keepalive: keepalive}
		section string
		peers   int
		err     error
	)
	for n, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = strings.ToLower(strings.Trim(line, "[] "))
			switch section {
			case "interface":
			case "peer":
				peers++
			default:
				return nil, fmt.Errorf("vpnclient: line %d: unknown section %q", n+1, line)
			}
			continue
		}
		// A key never contains '=', a value (base64) may end in it.
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("vpnclient: line %d: not a key = value line", n+1)
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch section + "/" + key {
		case "interface/privatekey":
			err = c.PrivateKey.UnmarshalText([]byte(value))
		case "interface/address":
			c.Addresses, err = appendPrefixes(c.Addresses, value)
		case "interface/dns":
			c.DNS = appendAddrs(c.DNS, value)
		case "interface/mtu":
			c.MTU, err = strconv.Atoi(value)
		case "peer/publickey":
			err = c.PublicKey.UnmarshalText([]byte(value))
		case "peer/presharedkey":
			err = c.PresharedKey.UnmarshalText([]byte(value))
		case "peer/endpoint":
			c.Endpoint = value
		case "peer/allowedips":
			c.AllowedIPs, err = appendPrefixes(c.AllowedIPs, value)
		case "peer/persistentkeepalive":
			c.Keepalive, err = strconv.Atoi(value)
		}
		if err != nil {
			return nil, fmt.Errorf("vpnclient: line %d: %s: %w", n+1, key, err)
		}
	}
	return c, c.validate(peers)
}

func appendPrefixes(list []netip.Prefix, value string) ([]netip.Prefix, error) {
	for _, s := range strings.Split(value, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, nil
}

// appendAddrs keeps the addresses of a DNS line; the search domains that may stand in it are of no use here.
func appendAddrs(list []netip.Addr, value string) []netip.Addr {
	for _, s := range strings.Split(value, ",") {
		if a, err := netip.ParseAddr(strings.TrimSpace(s)); err == nil {
			list = append(list, a)
		}
	}
	return list
}

func (c *Config) validate(peers int) error {
	switch {
	case peers != 1:
		return fmt.Errorf("vpnclient: want exactly one peer, found %d", peers)
	case c.PrivateKey.IsZero():
		return errors.New("vpnclient: no private key")
	case c.PublicKey.IsZero():
		return errors.New("vpnclient: no public key for the phone")
	case len(c.Addresses) == 0:
		return errors.New("vpnclient: no address for this device")
	case c.Endpoint == "":
		return errors.New("vpnclient: no endpoint")
	case len(c.AllowedIPs) == 0:
		return errors.New("vpnclient: no allowed IPs")
	case c.MTU < minMTU || c.MTU > maxMTU:
		return fmt.Errorf("vpnclient: MTU %d is out of range", c.MTU)
	case c.Keepalive < 0 || c.Keepalive > 3600:
		return fmt.Errorf("vpnclient: keepalive %d is out of range", c.Keepalive)
	}
	if _, err := netip.ParseAddrPort(c.Endpoint); err != nil {
		return fmt.Errorf("vpnclient: endpoint %q must be IP:port: %w", c.Endpoint, err)
	}
	return nil
}

// uapi renders c in wireguard-go's userspace configuration format.
func (c *Config) uapi() string {
	var b strings.Builder
	fmt.Fprintf(&b, "private_key=%s\nreplace_peers=true\npublic_key=%s\n", c.PrivateKey.Hex(), c.PublicKey.Hex())
	if !c.PresharedKey.IsZero() {
		fmt.Fprintf(&b, "preshared_key=%s\n", c.PresharedKey.Hex())
	}
	fmt.Fprintf(&b, "endpoint=%s\n", c.Endpoint)
	for _, p := range c.AllowedIPs {
		fmt.Fprintf(&b, "allowed_ip=%s\n", p)
	}
	if c.Keepalive > 0 {
		fmt.Fprintf(&b, "persistent_keepalive_interval=%d\n", c.Keepalive)
	}
	return b.String()
}
