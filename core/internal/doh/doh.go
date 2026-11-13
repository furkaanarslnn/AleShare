// Package doh answers the clients' DNS queries over HTTPS (RFC 8484), so the mobile operator, and anyone else
// on the way, cannot read which names the devices behind the phone look up. The servers are reached at fixed
// addresses: finding them needs no plain DNS lookup first.
package doh

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"sync/atomic"
	"time"
)

const (
	contentType  = "application/dns-message"
	maxMessage   = 65535
	queryTimeout = 4 * time.Second
	// Connections stay open between queries: one TLS handshake, then each query costs a round trip.
	idleConnTimeout = 90 * time.Second
)

// Dialer opens the outgoing connections (on a phone: bound to the cellular network).
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Server is one DoH endpoint, reached at a fixed IP address.
type Server struct {
	URL  string // e.g. https://cloudflare-dns.com/dns-query; its host name is what the certificate must match
	Addr string // e.g. 1.1.1.1:443
}

// DefaultServers are two independent operators, so one being unreachable does not stop name lookups.
var DefaultServers = []Server{
	{URL: "https://cloudflare-dns.com/dns-query", Addr: "1.1.1.1:443"},
	{URL: "https://cloudflare-dns.com/dns-query", Addr: "1.0.0.1:443"},
	{URL: "https://dns.quad9.net/dns-query", Addr: "9.9.9.9:443"},
}

// Resolver sends DNS messages to the first server that answers, and remembers which one that was.
type Resolver struct {
	servers  []Server
	clients  []*http.Client
	fallback netip.AddrPort
	dialer   Dialer
	next     atomic.Int32
}

// New makes a resolver. fallback, if valid, is a plain DNS server used only when no DoH server can be reached
// (a network that blocks them would otherwise have no names at all); tlsConfig is for tests and may be nil.
func New(dialer Dialer, servers []Server, fallback netip.AddrPort, tlsConfig *tls.Config) *Resolver {
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	r := &Resolver{servers: servers, fallback: fallback, dialer: dialer}
	for _, s := range servers {
		addr := s.Addr
		tr := &http.Transport{
			// Whatever host the URL names, connect to the fixed address.
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, addr)
			},
			TLSClientConfig:     tlsConfig,
			ForceAttemptHTTP2:   true,
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     idleConnTimeout,
			TLSHandshakeTimeout: queryTimeout,
		}
		r.clients = append(r.clients, &http.Client{Transport: tr, Timeout: queryTimeout})
	}
	return r
}

// Resolve answers one DNS message. encrypted tells whether it went over HTTPS or, as a last resort, in plain.
func (r *Resolver) Resolve(ctx context.Context, query []byte) (answer []byte, encrypted bool, err error) {
	if len(query) < 12 || len(query) > maxMessage {
		return nil, false, errors.New("doh: not a DNS message")
	}
	// RFC 8484 asks for ID 0, which also makes answers cacheable; the client gets its own ID back.
	id := [2]byte{query[0], query[1]}
	q := append([]byte(nil), query...)
	q[0], q[1] = 0, 0

	start := int(r.next.Load())
	var errs []error
	for k := range r.servers {
		i := (start + k) % len(r.servers)
		ans, err := r.ask(ctx, i, q)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		r.next.Store(int32(i))
		ans[0], ans[1] = id[0], id[1]
		return ans, true, nil
	}
	if r.fallback.IsValid() {
		ans, err := r.plain(ctx, query)
		if err == nil {
			return ans, false, nil
		}
		errs = append(errs, err)
	}
	return nil, false, fmt.Errorf("doh: no server answered: %w", errors.Join(errs...))
}

func (r *Resolver) ask(ctx context.Context, i int, q []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.servers[i].URL, bytes.NewReader(q))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", contentType)
	resp, err := r.clients[i].Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("doh: %s: %s", r.servers[i].Addr, resp.Status)
	}
	ans, err := io.ReadAll(io.LimitReader(resp.Body, maxMessage+1))
	if err != nil {
		return nil, err
	}
	if len(ans) < 12 || len(ans) > maxMessage {
		return nil, fmt.Errorf("doh: %s: answer of %d bytes", r.servers[i].Addr, len(ans))
	}
	return ans, nil
}

// plain asks the fallback server over UDP, with the client's own ID.
func (r *Resolver) plain(ctx context.Context, query []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	c, err := r.dialer.DialContext(ctx, "udp", r.fallback.String())
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if d, ok := ctx.Deadline(); ok {
		c.SetDeadline(d)
	}
	if _, err := c.Write(query); err != nil {
		return nil, err
	}
	buf := make([]byte, maxMessage)
	for {
		n, err := c.Read(buf)
		if err != nil {
			return nil, err
		}
		if n >= 12 && buf[0] == query[0] && buf[1] == query[1] {
			return append([]byte(nil), buf[:n]...), nil
		}
	}
}
