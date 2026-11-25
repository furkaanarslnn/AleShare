//go:build linux

package wgcore

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"syscall"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// Ping sends one echo request over the phone's mobile network (gateway.Pinger), with the "ping" socket every app
// may open on Android (an ICMP datagram socket: the kernel fills in the identifier and the checksum).
func (d protectedDialer) Ping(ctx context.Context, dst netip.Addr, payload []byte) error {
	family, proto := syscall.AF_INET, syscall.IPPROTO_ICMP
	var request, reply icmp.Type = ipv4.ICMPTypeEcho, ipv4.ICMPTypeEchoReply
	if dst.Is6() && !dst.Is4In6() {
		family, proto = syscall.AF_INET6, syscall.IPPROTO_ICMPV6
		request, reply = ipv6.ICMPTypeEchoRequest, ipv6.ICMPTypeEchoReply
	}
	fd, err := syscall.Socket(family, syscall.SOCK_DGRAM|syscall.SOCK_CLOEXEC, proto)
	if err != nil {
		return err
	}
	if !d.p.Protect(fd) {
		syscall.Close(fd)
		return errors.New("wgcore: the ping socket was not protected")
	}
	f := os.NewFile(uintptr(fd), "ping")
	conn, err := net.FilePacketConn(f)
	f.Close()
	if err != nil {
		return err
	}
	defer conn.Close()
	msg, err := (&icmp.Message{Type: request, Body: &icmp.Echo{Seq: 1, Data: payload}}).Marshal(nil)
	if err != nil {
		return err
	}
	if _, err := conn.WriteTo(msg, &net.UDPAddr{IP: dst.Unmap().AsSlice()}); err != nil {
		return err
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(4 * time.Second)
	}
	conn.SetReadDeadline(deadline)
	buf := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return err
		}
		m, err := icmp.ParseMessage(proto, buf[:n])
		if err != nil || m.Type != reply {
			continue
		}
		if e, ok := m.Body.(*icmp.Echo); ok && bytes.Equal(e.Data, payload) {
			return nil
		}
	}
}
