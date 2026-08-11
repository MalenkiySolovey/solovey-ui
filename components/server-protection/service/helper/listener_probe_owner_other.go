//go:build !linux

package helper

import (
	"context"
	"errors"
	"net/netip"
)

func probeListenerOwner(context.Context, netip.Addr, int, int) (ListenerOwner, error) {
	return "", errors.New("listener process ownership is unavailable on this platform")
}
