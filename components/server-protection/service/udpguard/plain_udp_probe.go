package udpguard

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/netip"
	"time"
)

// probePlainUDP is an internal health primitive. Payload and expectation come
// from a protocol-owned provider fixture; neither is accepted by the API.
func probePlainUDP(ctx context.Context, endpoint netip.AddrPort, request, expected []byte, limit int) error {
	if !endpoint.IsValid() || endpoint.Port() == 0 || len(request) == 0 || len(expected) == 0 || limit < len(expected) || limit > 4096 {
		return errors.New("plain_udp_probe_contract_invalid")
	}
	dialer := net.Dialer{}
	connection, err := dialer.DialContext(ctx, "udp", endpoint.String())
	if err != nil {
		return err
	}
	defer connection.Close()
	deadline := time.Now().Add(500 * time.Millisecond)
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	if err = connection.SetDeadline(deadline); err != nil {
		return err
	}
	if _, err = connection.Write(request); err != nil {
		return err
	}
	response := make([]byte, limit)
	count, err := connection.Read(response)
	if err != nil {
		return err
	}
	if !bytes.Equal(response[:count], expected) {
		return errors.New("plain_udp_probe_response_mismatch")
	}
	return nil
}
