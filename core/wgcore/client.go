package wgcore

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"hotspot/core/internal/vpnclient"
	"hotspot/core/internal/wifiinfo"
)

// Join is what a join link carries: the Wi-Fi network to enter and the tunnel to open. The lists are
// comma separated, which is all the app needs to set up its VPN.
type Join struct {
	SSID     string
	Password string
	// Config is the WireGuard configuration text. It holds this device's private key.
	Config    string
	Endpoint  string // IP:port of the phone that shares
	Addresses string // this device's addresses in the tunnel, as CIDR
	DNS       string
	Routes    string // what goes through the tunnel, as CIDR
	MTU       int
}

// ParseJoin reads a join link, as scanned from a QR code or pasted.
func ParseJoin(text string) (*Join, error) {
	j, err := vpnclient.ParseJoin(text)
	if err != nil {
		return nil, err
	}
	c := j.Config
	out := &Join{SSID: j.SSID, Password: j.Password, Config: j.Text, Endpoint: c.Endpoint, MTU: c.MTU}
	var parts []string
	for _, p := range c.Addresses {
		parts = append(parts, p.String())
	}
	out.Addresses, parts = strings.Join(parts, ","), nil
	for _, a := range c.DNS {
		parts = append(parts, a.String())
	}
	out.DNS, parts = strings.Join(parts, ","), nil
	for _, p := range c.AllowedIPs {
		parts = append(parts, p.String())
	}
	out.Routes = strings.Join(parts, ",")
	return out, nil
}

// EncodeJoin makes the join link that goes into a QR code, after checking that it would work.
func EncodeJoin(ssid, password, config string) (string, error) {
	return vpnclient.EncodeJoin(ssid, password, config)
}

// ClientTunnel is this device's side of a tunnel to a phone that shares.
type ClientTunnel struct{ t *vpnclient.Tunnel }

// Stop takes the tunnel down and closes the device it ran on.
func (c *ClientTunnel) Stop() { c.t.Close() }

// RxBytes is what came in through the tunnel.
func (c *ClientTunnel) RxBytes() int64 { return c.stats().Rx }

// TxBytes is what went out through the tunnel.
func (c *ClientTunnel) TxBytes() int64 { return c.stats().Tx }

// HandshakeAge is the seconds since the phone last answered, or -1 if it never has. A tunnel without a
// recent handshake is not connected, whatever the VPN icon says.
func (c *ClientTunnel) HandshakeAge() int64 {
	h := c.stats().Handshake
	if h.IsZero() {
		return -1
	}
	return int64(time.Since(h) / time.Second)
}

func (c *ClientTunnel) stats() vpnclient.Stats {
	s, _ := c.t.Stats()
	return s
}

// WifiNetworks is what the phone says about its Wi-Fi network: the one it opens and, while the user changes
// it, the one that follows (empty when there is none).
type WifiNetworks struct {
	SSID, Password         string
	NextSSID, NextPassword string
	// The newest app the phone has (0 and empty when it offers none): a device with a lower code can download it
	// from UpdateURL, over the phone's Wi-Fi network, and one with a higher code can bring its own there (a POST
	// with &code=N, see SetAppUpdate).
	AppCode   int
	AppName   string
	UpdateURL string
}

// askWifiTimeout bounds AskWifi; the phone answers within milliseconds when it is there.
const askWifiTimeout = 3 * time.Second

// AskWifi asks the phone for its Wi-Fi network. config is this device's WireGuard configuration, whose keys
// prove to the phone that this device is paired. The request goes to the phone's address on its Wi-Fi network,
// over a socket that protector binds to that network (nil: the system's routing decides). It blocks for at most
// a few seconds.
func AskWifi(config string, protector Protector) (*WifiNetworks, error) {
	cfg, err := vpnclient.Parse(config)
	if err != nil {
		return nil, err
	}
	endpoint, err := netip.ParseAddrPort(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	lc := net.ListenConfig{Control: func(_, _ string, rc syscall.RawConn) error {
		if protector == nil {
			return nil
		}
		ok := false
		if err := rc.Control(func(fd uintptr) { ok = protector.Protect(int(fd)) }); err != nil {
			return err
		}
		if !ok {
			return errors.New("wgcore: the socket could not be bound to the network")
		}
		return nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), askWifiTimeout)
	defer cancel()
	conn, err := lc.ListenPacket(ctx, "udp4", ":0")
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	phone := net.UDPAddrFromAddrPort(netip.AddrPortFrom(endpoint.Addr(), wifiinfo.Port))
	info, err := wifiinfo.Ask(ctx, conn, phone, cfg.PrivateKey, cfg.PublicKey)
	if err != nil {
		return nil, err
	}
	out := &WifiNetworks{SSID: info.Current.SSID, Password: info.Current.Password}
	if info.Next != nil {
		out.NextSSID, out.NextPassword = info.Next.SSID, info.Next.Password
	}
	if a := info.App; a != nil && a.Code > 0 && a.Token != "" {
		out.AppCode, out.AppName = a.Code, a.Name
		out.UpdateURL = updateURL(endpoint.Addr(), a.Token)
	}
	return out, nil
}

// updateURL is where the phone at addr hands out its APK.
func updateURL(addr netip.Addr, token string) string {
	return (&url.URL{
		Scheme:   "http",
		Host:     netip.AddrPortFrom(addr, UpdatePort).String(),
		Path:     "/hotspot.apk",
		RawQuery: url.Values{"t": {token}}.Encode(),
	}).String()
}
