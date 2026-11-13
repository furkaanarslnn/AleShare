package vpnclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"syscall"

	"golang.zx2c4.com/wireguard/conn"
)

// ProtectFunc is called with the descriptor of every socket the tunnel opens, before the socket is used. On
// Android it binds the socket to the phone's Wi-Fi network: Go makes its system calls itself, so Android's
// own "use this network for the whole process" never reaches these sockets, and the phone's network has no
// internet that would make it the default one.
type ProtectFunc func(fd int) error

// NewBind returns the tunnel's way to the network: one UDP socket, opened through protect. A nil protect makes
// it a plain socket.
func NewBind(protect ProtectFunc) conn.Bind { return &udpBind{protect: protect} }

type udpBind struct {
	protect ProtectFunc

	mu   sync.Mutex
	sock *net.UDPConn
}

var errBindOpen = errors.New("vpnclient: the socket is already open")

func (b *udpBind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sock != nil {
		return nil, 0, errBindOpen
	}
	lc := net.ListenConfig{Control: func(_, _ string, rc syscall.RawConn) error {
		if b.protect == nil {
			return nil
		}
		var perr error
		if err := rc.Control(func(fd uintptr) { perr = b.protect(int(fd)) }); err != nil {
			return err
		}
		return perr
	}}
	pc, err := lc.ListenPacket(context.Background(), "udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, 0, err
	}
	sock := pc.(*net.UDPConn)
	b.sock = sock
	actual := uint16(sock.LocalAddr().(*net.UDPAddr).Port)

	receive := func(packets [][]byte, sizes []int, eps []conn.Endpoint) (int, error) {
		n, from, err := sock.ReadFromUDPAddrPort(packets[0])
		if err != nil {
			return 0, err
		}
		sizes[0] = n
		// A dual-stack socket reports an IPv4 sender as an IPv4-mapped IPv6 address.
		eps[0] = &conn.StdNetEndpoint{AddrPort: netip.AddrPortFrom(from.Addr().Unmap(), from.Port())}
		return 1, nil
	}
	return []conn.ReceiveFunc{receive}, actual, nil
}

func (b *udpBind) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sock == nil {
		return nil
	}
	err := b.sock.Close()
	b.sock = nil
	return err
}

func (b *udpBind) SetMark(uint32) error { return nil }

func (b *udpBind) BatchSize() int { return 1 }

func (b *udpBind) ParseEndpoint(s string) (conn.Endpoint, error) {
	ap, err := netip.ParseAddrPort(s)
	if err != nil {
		return nil, err
	}
	return &conn.StdNetEndpoint{AddrPort: ap}, nil
}

func (b *udpBind) Send(bufs [][]byte, ep conn.Endpoint) error {
	e, ok := ep.(*conn.StdNetEndpoint)
	if !ok {
		return conn.ErrWrongEndpointType
	}
	b.mu.Lock()
	sock := b.sock
	b.mu.Unlock()
	if sock == nil {
		return net.ErrClosed
	}
	for _, buf := range bufs {
		if _, err := sock.WriteToUDPAddrPort(buf, e.AddrPort); err != nil {
			return err
		}
	}
	return nil
}
