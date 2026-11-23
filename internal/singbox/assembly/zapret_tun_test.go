package assembly

import (
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"

	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	"github.com/sagernet/sing-tun"
)

func TestZapretTUNProjection(t *testing.T) {
	const managed = `{"type":"tun","tag":"ingress","interface_name":"svzap-tun0","address":["192.0.2.1/30"],"mtu":1500,"stack":"gvisor","dns_mode":"disabled"}`
	const normal = `{"type":"tun","tag":"another","interface_name":"normal-tun"}`
	choose := func(bound bool, options ...string) error {
		inbounds := make([]json.RawMessage, 0, len(options))
		for _, value := range options {
			inbounds = append(inbounds, json.RawMessage(value))
		}
		request, err := installedZapretTUNRequest(inbounds, bound)
		if request != nil && (request.Tag != "ingress" || request.Address.String() != "192.0.2.1/30" || request.MTU != 1500) {
			t.Fatalf("unexpected admission request: %+v", request)
		}
		return err
	}
	if err := choose(false, normal); err != nil {
		t.Fatalf("unbound ordinary TUN changed: %v", err)
	}
	for _, input := range []struct {
		bound bool
		rows  []string
	}{
		{false, []string{managed}},
		{true, []string{normal}},
		{true, []string{managed, normal}},
		{true, []string{strings.Replace(managed, `"stack":`, `"auto_route":true,"stack":`, 1)}},
		{true, []string{strings.Replace(managed, `"stack":`, `"file_descriptor":4,"stack":`, 1)}},
	} {
		if err := choose(input.bound, input.rows...); !errors.Is(err, entityoutbounds.ErrZapretPacketPathUnqualified) {
			t.Fatalf("unsafe TUN candidate was not rejected: %+v, err=%v", input, err)
		}
	}
	err := choose(true, managed)
	if runtime.GOOS == "linux" && tun.WithGVisor {
		if err != nil {
			t.Fatal(err)
		}
	} else if !errors.Is(err, entityoutbounds.ErrZapretPacketPathUnqualified) {
		t.Fatalf("missing Linux gVisor capability was not rejected: %v", err)
	}
}
