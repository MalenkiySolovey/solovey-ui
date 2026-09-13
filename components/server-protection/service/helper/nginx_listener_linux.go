//go:build linux

package helper

import (
	"context"
	"errors"
	"net/netip"
	"strconv"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/listenerevidence"
)

func platformNginxOwnsListeners(ctx context.Context, pids []int, expected []NginxListener) error {
	allowedPorts := make(map[uint16]bool, len(expected))
	wanted := make(map[string]bool, len(expected))
	for _, listener := range expected {
		address, err := netip.ParseAddr(listener.Address)
		if err != nil || listener.Port <= 0 || listener.Port > 65535 {
			return errors.New("expected nginx listener identity is invalid")
		}
		allowedPorts[uint16(listener.Port)] = true
		wanted[nginxListenerKey(address, uint16(listener.Port))] = false
	}
	for _, pid := range pids {
		observation, err := listenerevidence.ObserveProcessSockets(ctx, pid, hostfacts.NetworkTCP, allowedPorts)
		if err != nil {
			return errors.New("nginx listener process identity is unavailable")
		}
		for _, socket := range observation.Sockets {
			address, err := netip.ParseAddr(socket.Bind)
			if err != nil {
				continue
			}
			key := nginxListenerKey(address, socket.Port)
			if _, exists := wanted[key]; exists {
				wanted[key] = true
			}
		}
	}
	for _, found := range wanted {
		if !found {
			return errors.New("expected nginx listener ownership mismatch")
		}
	}
	return nil
}

func nginxListenerKey(address netip.Addr, port uint16) string {
	return address.String() + ":" + strconv.Itoa(int(port))
}
