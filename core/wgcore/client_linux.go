//go:build linux

package wgcore

import (
	"errors"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/tun"

	"hotspot/core/internal/vpnclient"
)

// StartClient opens the tunnel described by a WireGuard configuration on the VPN's TUN device. tunFd is the
// detached descriptor of the device that VpnService made; this takes it over and closes it on Stop. The UDP
// socket to the phone must not run into the tunnel itself, which the app makes sure of by excluding itself
// from its own VPN. protector, if given, is asked to bind that socket to the phone's Wi-Fi network (nil: leave
// it to the system's routing).
func StartClient(config string, tunFd int, protector Protector) (*ClientTunnel, error) {
	cfg, err := vpnclient.Parse(config)
	if err != nil {
		return nil, err
	}
	dev, _, err := tun.CreateUnmonitoredTUNFromFD(tunFd)
	if err != nil {
		return nil, err
	}
	var bind conn.Bind
	if protector != nil {
		bind = vpnclient.NewBind(func(fd int) error {
			if !protector.Protect(fd) {
				return errors.New("wgcore: the tunnel's socket could not be bound to the network")
			}
			return nil
		})
	}
	t, err := vpnclient.Start(dev, cfg, bind)
	if err != nil {
		return nil, err
	}
	return &ClientTunnel{t: t}, nil
}
