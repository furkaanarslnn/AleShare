//go:build !linux

package wgcore

import (
	"context"
	"errors"
	"net/netip"
)

// Ping needs Android's "ping" sockets (see ping_linux.go); elsewhere the gateway drops pings as before.
func (d protectedDialer) Ping(ctx context.Context, dst netip.Addr, payload []byte) error {
	return errors.ErrUnsupported
}
