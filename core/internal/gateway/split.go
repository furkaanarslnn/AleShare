package gateway

import (
	"net"
	"sync/atomic"
	"time"
)

// splitPause is how long the pieces of a split start are held apart, so that the kernel sends each as a segment
// of its own instead of merging two that are written back to back (it may, to save packets).
var splitPause = time.Millisecond

// splitConn is the outgoing end of a TCP flow whose first bytes, if they open a TLS handshake, go out in pieces.
// Some networks' filters read the name of the site from the first segment of the handshake and cut the
// connection; one that does not find a whole handshake there lets it pass. Everything after the first write, and
// every flow that does not start with a handshake, goes through unchanged.
type splitConn struct {
	net.Conn
	count *atomic.Int64 // flows that were split
	done  bool          // the first write has been seen; only the pump from the client to the internet writes
}

func (c *splitConn) Write(b []byte) (int, error) {
	if c.done {
		return c.Conn.Write(b)
	}
	c.done = true
	cuts := splitPoints(b)
	if len(cuts) == 0 {
		return c.Conn.Write(b)
	}
	c.count.Add(1)
	written, from := 0, 0
	for i, to := range append(cuts, len(b)) {
		if i > 0 {
			time.Sleep(splitPause)
		}
		n, err := c.Conn.Write(b[from:to])
		written += n
		if err != nil {
			return written, err
		}
		from = to
	}
	return written, nil
}

// CloseWrite half-closes the flow like the connection underneath does; one that cannot is closed whole, which is
// what the relay does for connections without CloseWrite.
func (c *splitConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return c.Conn.Close()
}

// splitPoints says where to cut the first bytes of a flow: after the first byte, and in the middle of the site's
// name when the name is among those bytes. No points for anything that is not the start of a TLS handshake
// (a ClientHello), so that other protocols are left alone.
func splitPoints(b []byte) []int {
	// TLS record: type 22 (handshake), version 3.x; handshake message: type 1 (ClientHello).
	if len(b) < 6 || b[0] != 0x16 || b[1] != 0x03 || b[5] != 0x01 {
		return nil
	}
	cuts := []int{1}
	if start, end, ok := serverName(b); ok && end-start >= 2 {
		cuts = append(cuts, start+(end-start)/2)
	}
	return cuts
}

// serverName finds the host name of the server_name extension in a ClientHello that begins b, which may stop short
// of the extension (the first segment is all there is); b[start:end] is the name.
func serverName(b []byte) (start, end int, ok bool) {
	// Record header (5) and handshake header (4), client version (2), random (32).
	p := 5 + 4 + 2 + 32
	skip := func(lenBytes int) bool { // a length-prefixed field
		if p+lenBytes > len(b) {
			return false
		}
		n := 0
		for _, v := range b[p : p+lenBytes] {
			n = n<<8 | int(v)
		}
		p += lenBytes + n
		return p <= len(b)
	}
	if !skip(1) || !skip(2) || !skip(1) { // session id, cipher suites, compression methods
		return 0, 0, false
	}
	if p+2 > len(b) {
		return 0, 0, false
	}
	p += 2 // total length of the extensions
	for p+4 <= len(b) {
		typ := int(b[p])<<8 | int(b[p+1])
		size := int(b[p+2])<<8 | int(b[p+3])
		p += 4
		if typ != 0 { // not server_name
			p += size
			continue
		}
		// List length (2), then entries: name type (1, 0 = host name), length (2), name.
		if p+5 > len(b) || b[p+2] != 0 {
			return 0, 0, false
		}
		n := int(b[p+3])<<8 | int(b[p+4])
		start, end = p+5, p+5+n
		return start, end, end <= len(b) && n > 0
	}
	return 0, 0, false
}
