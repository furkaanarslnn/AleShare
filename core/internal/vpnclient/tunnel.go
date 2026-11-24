package vpnclient

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// Tunnel is a running client.
type Tunnel struct {
	dev *device.Device
}

// Start brings the tunnel up on dev, which it takes over: Close closes it. bind is how it reaches the network
// ([NewBind]); nil means the standard one.
func Start(dev tun.Device, c *Config, bind conn.Bind) (*Tunnel, error) {
	if bind == nil {
		bind = conn.NewDefaultBind()
	}
	d := device.NewDevice(dev, bind, device.NewLogger(device.LogLevelError, "client: "))
	if err := d.IpcSet(c.uapi()); err != nil {
		d.Close()
		return nil, fmt.Errorf("vpnclient: configure: %w", err)
	}
	if err := d.Up(); err != nil {
		d.Close()
		return nil, fmt.Errorf("vpnclient: up: %w", err)
	}
	return &Tunnel{dev: d}, nil
}

// Close takes the tunnel down.
func (t *Tunnel) Close() { t.dev.Close() }

// Stats is what the tunnel has done so far.
type Stats struct {
	Rx, Tx    int64
	Handshake time.Time // zero until the phone has answered
}

// Stats reads the counters and the time of the last handshake. A handshake means the phone is there and the
// keys fit: it is what tells a working tunnel from one that merely exists.
func (t *Tunnel) Stats() (Stats, error) {
	text, err := t.dev.IpcGet()
	if err != nil {
		return Stats{}, err
	}
	var (
		s        Stats
		sec, ns  int64
		haveTime bool
	)
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		n, _ := strconv.ParseInt(value, 10, 64)
		switch key {
		case "rx_bytes":
			s.Rx = n
		case "tx_bytes":
			s.Tx = n
		case "last_handshake_time_sec":
			sec, haveTime = n, true
		case "last_handshake_time_nsec":
			ns = n
		}
	}
	if haveTime && sec > 0 {
		s.Handshake = time.Unix(sec, ns)
	}
	return s, nil
}
