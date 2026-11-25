package gateway

import (
	"context"
	"net/netip"
	"time"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip/checksum"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// Ping: a client's echo request to an address on the internet is sent from the phone with an echo socket of its
// own (Options.Pinger), and the answer goes back to the client as if the address had answered. Without it the
// stack drops the request: "ping" never answered, though everything else worked, and a ping is how people tell.
// The phone's own addresses are left to the stack, which answers them itself.

// Pinger sends one echo request with payload to dst and returns nil once the matching reply came back, before ctx
// ends.
type Pinger interface {
	Ping(ctx context.Context, dst netip.Addr, payload []byte) error
}

const (
	// pingTimeout is how long a reply is waited for; the client's ping gives up about as soon.
	pingTimeout = 4 * time.Second
	// maxClientPings is how many echo requests of one device may wait for their reply at once.
	maxClientPings = 16
)

// echo takes a client's echo request to the internet out of the packets the stack gets, and reports whether it
// did. It answers it through Options.Pinger, in the background; a request that may not go (a paused device, an
// address the client may not reach, too many waiting) is dropped, as it was before.
func (g *Gateway) echo(packet []byte) bool {
	if g.opts.Pinger == nil || len(packet) == 0 {
		return false
	}
	var src, dst netip.Addr
	var msg []byte
	switch packet[0] >> 4 {
	case 4:
		h := header.IPv4(packet)
		if !h.IsValid(len(packet)) || h.Protocol() != uint8(header.ICMPv4ProtocolNumber) || h.More() || h.FragmentOffset() != 0 {
			return false
		}
		msg = h.Payload()
		if len(msg) < header.ICMPv4MinimumSize || header.ICMPv4(msg).Type() != header.ICMPv4Echo {
			return false
		}
		src, dst = netip.AddrFrom4(h.SourceAddress().As4()), netip.AddrFrom4(h.DestinationAddress().As4())
	case 6:
		h := header.IPv6(packet)
		if !h.IsValid(len(packet)) || h.NextHeader() != uint8(header.ICMPv6ProtocolNumber) {
			return false
		}
		msg = h.Payload()
		if len(msg) < header.ICMPv6EchoMinimumSize || header.ICMPv6(msg).Type() != header.ICMPv6EchoRequest {
			return false
		}
		src, dst = netip.AddrFrom16(h.SourceAddress().As16()), netip.AddrFrom16(h.DestinationAddress().As16())
	default:
		return false
	}
	if _, own := g.addrs[dst]; own {
		return false // the phone itself: the stack answers
	}
	// From here on the request is this one's: answered or dropped, never handed to the stack (which drops it).
	if !g.allowed(dst) || g.paused(src) || !g.pings.begin(src) {
		return true
	}
	request := append([]byte(nil), packet...)
	payload := append([]byte(nil), msg[8:]...) // after type, code, checksum, identifier and sequence
	go func() {
		defer g.pings.end(src)
		ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
		defer cancel()
		if g.opts.Pinger.Ping(ctx, dst, payload) != nil {
			return // no answer: the client's ping says so itself
		}
		if reply := echoReply(request); reply != nil {
			g.toClient(reply)
		}
	}()
	return true
}

// echoReply turns an echo request (a whole IP packet) into the reply the client expects: addresses swapped, the
// type changed, the identifier, sequence and data kept, checksums made anew.
func echoReply(p []byte) []byte {
	switch p[0] >> 4 {
	case 4:
		h := header.IPv4(p)
		src, dst := h.SourceAddress(), h.DestinationAddress()
		h.SetSourceAddress(dst)
		h.SetDestinationAddress(src)
		h.SetTTL(64)
		h.SetChecksum(0)
		h.SetChecksum(^h.CalculateChecksum())
		m := header.ICMPv4(h.Payload())
		m.SetType(header.ICMPv4EchoReply)
		m.SetCode(0)
		m.SetChecksum(0)
		m.SetChecksum(^checksum.Checksum(m, 0))
		return p
	case 6:
		h := header.IPv6(p)
		src, dst := h.SourceAddress(), h.DestinationAddress()
		h.SetSourceAddress(dst)
		h.SetDestinationAddress(src)
		h.SetHopLimit(64)
		m := header.ICMPv6(h.Payload())
		m.SetType(header.ICMPv6EchoReply)
		m.SetCode(0)
		m.SetChecksum(header.ICMPv6Checksum(header.ICMPv6ChecksumParams{Header: m, Src: dst, Dst: src}))
		return p
	}
	return nil
}

// toClient queues a whole IP packet for the clients, as the stack queues its own (see Read).
func (g *Gateway) toClient(packet []byte) {
	select {
	case <-g.done:
		return
	default:
	}
	pkb := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(packet)})
	var list stack.PacketBufferList
	list.PushBack(pkb)
	g.ep.WritePackets(list)
	pkb.DecRef()
}
