package vpnclient_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"

	"hotspot/core/internal/gateway"
	"hotspot/core/internal/provision"
	"hotspot/core/internal/server"
	"hotspot/core/internal/vpnclient"
)

// confFor registers a client on a fresh state and returns the configuration the phone would hand out.
func confFor(t *testing.T, endpoint string) (state *provision.State, conf string) {
	t.Helper()
	state = provision.NewState()
	p, priv, err := state.AddPeer("tablet")
	if err != nil {
		t.Fatal(err)
	}
	return state, provision.ClientConfig(priv, p, state.ServerPublicKey(), endpoint)
}

func TestParseWhatThePhoneWrites(t *testing.T) {
	_, conf := confFor(t, "192.168.49.1:51820")
	c, err := vpnclient.Parse(conf)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Addresses) != 2 || len(c.DNS) != 2 || len(c.AllowedIPs) != 2 {
		t.Fatalf("addresses %v, dns %v, allowed %v", c.Addresses, c.DNS, c.AllowedIPs)
	}
	if c.Endpoint != "192.168.49.1:51820" || c.MTU != provision.TunnelMTU || c.PresharedKey.IsZero() {
		t.Fatalf("endpoint %q, mtu %d, psk zero %v", c.Endpoint, c.MTU, c.PresharedKey.IsZero())
	}
	if c.Keepalive == 0 {
		t.Fatal("a phone that vanishes must be noticed: a keepalive is needed")
	}
}

func TestParseRejectsWhatCannotWork(t *testing.T) {
	_, conf := confFor(t, "192.168.49.1:51820")
	cases := map[string]string{
		"no peer":          conf[:strings.Index(conf, "[Peer]")],
		"two peers":        conf + "\n[Peer]\nPublicKey = " + base64.StdEncoding.EncodeToString(make([]byte, 32)) + "\n",
		"no endpoint":      strings.Replace(conf, "Endpoint = 192.168.49.1:51820", "", 1),
		"host name":        strings.Replace(conf, "192.168.49.1:51820", "phone.example:51820", 1),
		"short key":        strings.Replace(conf, "PrivateKey = ", "PrivateKey = AAAA", 1),
		"huge MTU":         strings.Replace(conf, fmt.Sprintf("MTU = %d", provision.TunnelMTU), "MTU = 9000", 1),
		"unknown section":  conf + "\n[Script]\nPostUp = rm -rf\n",
		"line without key": conf + "\njust some words\n",
		"empty":            "",
	}
	for name, text := range cases {
		if _, err := vpnclient.Parse(text); err == nil {
			t.Errorf("%s: parsed, but must not", name)
		}
	}
}

func TestParseToleratesComments(t *testing.T) {
	_, conf := confFor(t, "192.168.49.1:51820")
	text := "# made by a phone\r\n" + strings.ReplaceAll(conf, "\n", " # note\r\n")
	if _, err := vpnclient.Parse(text); err != nil {
		t.Fatalf("comments and Windows line breaks must be fine: %v", err)
	}
}

func TestJoinRoundTrip(t *testing.T) {
	_, conf := confFor(t, "192.168.49.1:51820")
	link, err := vpnclient.EncodeJoin("DIRECT-HS-Q4TZ", "abcdEFGH2345jkmn", conf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(link, "hotspot://join?d=") || strings.ContainsAny(strings.TrimPrefix(link, "hotspot://join?d="), " \n+/=") {
		t.Fatalf("a link must survive a QR code and a chat message: %q", link)
	}
	for _, text := range []string{link, "  " + link + "\r\n", link + "=="} {
		j, err := vpnclient.ParseJoin(text)
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		if j.SSID != "DIRECT-HS-Q4TZ" || j.Password != "abcdEFGH2345jkmn" || j.Text != conf || j.Config.Endpoint != "192.168.49.1:51820" {
			t.Fatalf("parsed %+v", j)
		}
	}
}

func TestJoinRejects(t *testing.T) {
	_, conf := confFor(t, "192.168.49.1:51820")
	good, _ := vpnclient.EncodeJoin("DIRECT-HS-Q4TZ", "abcdEFGH2345jkmn", conf)
	enc := func(json string) string {
		return "hotspot://join?d=" + base64.RawURLEncoding.EncodeToString([]byte(json))
	}
	cases := map[string]string{
		"other scheme":   strings.Replace(good, "hotspot://", "https://", 1),
		"not base64":     "hotspot://join?d=!!!",
		"not json":       enc("hello"),
		"future format":  enc(`{"v":2,"s":"x","p":"12345678","c":""}`),
		"no name":        enc(`{"v":1,"s":"","p":"12345678","c":"x"}`),
		"short password": enc(`{"v":1,"s":"net","p":"1234","c":"x"}`),
		// A password Android refuses outright: saved, it would end the app at every start.
		"password with other letters": enc(`{"v":1,"s":"net","p":"şifreşifre","c":` + strconv.Quote(conf) + `}`),
		"password with a line break":  enc(`{"v":1,"s":"net","p":"1234\n5678","c":` + strconv.Quote(conf) + `}`),
		"bad config":                  enc(`{"v":1,"s":"net","p":"12345678","c":"[Interface]"}`),
		"too long":                    good + strings.Repeat("A", 9000),
	}
	for name, text := range cases {
		if _, err := vpnclient.ParseJoin(text); err == nil {
			t.Errorf("%s: parsed, but must not", name)
		}
	}
	if _, err := vpnclient.EncodeJoin("net", "123", conf); err == nil {
		t.Error("EncodeJoin must not make a link that ParseJoin would refuse")
	}
}

// A client built from what the phone hands out reaches a real gateway over a real tunnel, and says so.
func TestClientReachesTheGateway(t *testing.T) {
	echo := startEcho(t)

	l, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.LocalAddr().(*net.UDPAddr).Port
	l.Close()

	state, conf := confFor(t, fmt.Sprintf("127.0.0.1:%d", port))
	srv, err := server.Start(server.Config{
		PrivateKey: state.ServerKey,
		ListenPort: uint16(port),
		Peers:      state.ServerPeers(),
		Gateway: gateway.Options{
			Addrs:  []netip.Addr{provision.GatewayV4, provision.GatewayV6},
			Dialer: echoDialer{echo},
		},
		LogLevel: device.LogLevelError,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	cfg, err := vpnclient.Parse(conf)
	if err != nil {
		t.Fatal(err)
	}
	var addrs []netip.Addr
	for _, p := range cfg.Addresses {
		addrs = append(addrs, p.Addr())
	}
	tun, tnet, err := netstack.CreateNetTUN(addrs, cfg.DNS, cfg.MTU)
	if err != nil {
		t.Fatal(err)
	}
	tunnel, err := vpnclient.Start(tun, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	st, _ := tunnel.Stats()
	if !st.Handshake.IsZero() {
		t.Fatal("no handshake can have happened before any packet was sent")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := tnet.DialContextTCPAddrPort(ctx, netip.MustParseAddrPort("203.0.113.5:80"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("through the tunnel")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len("through the tunnel"))
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "through the tunnel" {
		t.Fatalf("echo = %q, %v", got, err)
	}

	st, err = tunnel.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Handshake.IsZero() || time.Since(st.Handshake) > time.Minute || st.Rx == 0 || st.Tx == 0 {
		t.Fatalf("stats after traffic: %+v", st)
	}
}

// The tunnel's own socket layer carries real traffic, and the socket goes through protect first: that is where the
// app binds it to the phone's Wi-Fi network, which is the only way a Go socket ever reaches that network.
func TestOwnSocketLayerWithProtect(t *testing.T) {
	echo := startEcho(t)
	l, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.LocalAddr().(*net.UDPAddr).Port
	l.Close()

	state, conf := confFor(t, fmt.Sprintf("127.0.0.1:%d", port))
	srv, err := server.Start(server.Config{
		PrivateKey: state.ServerKey,
		ListenPort: uint16(port),
		Peers:      state.ServerPeers(),
		Gateway:    gateway.Options{Addrs: []netip.Addr{provision.GatewayV4, provision.GatewayV6}, Dialer: echoDialer{echo}},
		LogLevel:   device.LogLevelError,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	cfg, _ := vpnclient.Parse(conf)
	tun, tnet, err := netstack.CreateNetTUN([]netip.Addr{cfg.Addresses[0].Addr()}, cfg.DNS, cfg.MTU)
	if err != nil {
		t.Fatal(err)
	}
	var protected atomic.Int32
	tunnel, err := vpnclient.Start(tun, cfg, vpnclient.NewBind(func(fd int) error {
		if fd <= 0 {
			return fmt.Errorf("bad descriptor %d", fd)
		}
		protected.Add(1)
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := tnet.DialContextTCPAddrPort(ctx, netip.MustParseAddrPort("203.0.113.5:80"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte("ping"))
	got := make([]byte, 4)
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "ping" {
		t.Fatalf("echo = %q, %v", got, err)
	}
	if protected.Load() == 0 {
		t.Fatal("the socket was not handed to protect")
	}
	if st, _ := tunnel.Stats(); st.Handshake.IsZero() {
		t.Fatal("no handshake over the tunnel's own socket layer")
	}
}

// If protect refuses, the tunnel must not come up half way: no socket, so no handshake, and Start says so or the
// device stays silent.
func TestProtectRefusalMeansNoTraffic(t *testing.T) {
	_, conf := confFor(t, "127.0.0.1:9")
	cfg, _ := vpnclient.Parse(conf)
	tun, _, err := netstack.CreateNetTUN([]netip.Addr{cfg.Addresses[0].Addr()}, cfg.DNS, cfg.MTU)
	if err != nil {
		t.Fatal(err)
	}
	tunnel, err := vpnclient.Start(tun, cfg, vpnclient.NewBind(func(int) error { return fmt.Errorf("no network") }))
	if err == nil {
		defer tunnel.Close()
		if st, _ := tunnel.Stats(); !st.Handshake.IsZero() {
			t.Fatal("a handshake without a socket")
		}
	}
}

// A client whose phone is not there never gets a handshake: that is what the app must show as "not connected".
func TestNoHandshakeWithoutAPhone(t *testing.T) {
	l, err := net.ListenPacket("udp", "127.0.0.1:0") // a port nobody serves
	if err != nil {
		t.Fatal(err)
	}
	port := l.LocalAddr().(*net.UDPAddr).Port
	defer l.Close()

	_, conf := confFor(t, fmt.Sprintf("127.0.0.1:%d", port))
	cfg, _ := vpnclient.Parse(conf)
	tun, tnet, err := netstack.CreateNetTUN([]netip.Addr{cfg.Addresses[0].Addr()}, cfg.DNS, cfg.MTU)
	if err != nil {
		t.Fatal(err)
	}
	tunnel, err := vpnclient.Start(tun, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tunnel.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if c, err := tnet.DialContextTCPAddrPort(ctx, netip.MustParseAddrPort("203.0.113.5:80")); err == nil {
		c.Close()
		t.Fatal("a dial must not succeed with nobody on the other end")
	}
	if st, _ := tunnel.Stats(); !st.Handshake.IsZero() {
		t.Fatalf("handshake without a phone: %+v", st)
	}
}

type echoDialer struct{ addr string }

func (d echoDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	var nd net.Dialer
	return nd.DialContext(ctx, "tcp", d.addr)
}

func startEcho(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	return l.Addr().String()
}

// A password of plain characters with a space or a sign in it is as good as any.
func TestJoinTakesAnyPrintablePassword(t *testing.T) {
	_, conf := confFor(t, "192.168.49.1:51820")
	link, err := vpnclient.EncodeJoin("DIRECT-Ev", `a b~!"#$%&'()`, conf)
	if err != nil {
		t.Fatal(err)
	}
	if j, err := vpnclient.ParseJoin(link); err != nil || j.Password != `a b~!"#$%&'()` {
		t.Fatalf("%+v, %v", j, err)
	}
	if _, err := vpnclient.EncodeJoin("DIRECT-Ev", "şifreşifre", conf); err == nil {
		t.Error("EncodeJoin made a link with a password Android refuses")
	}
}
