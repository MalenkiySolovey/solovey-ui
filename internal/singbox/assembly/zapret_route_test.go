package assembly

import (
	"errors"
	"strings"
	"testing"

	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
)

func TestZapretInterfaceRouteDefaults(t *testing.T) {
	for _, tc := range []struct {
		name, route, dial, want string
	}{
		{"unrelated fixed default", `"default_interface":"lan0"`, `,"bind_interface":"wan0"`, ""},
		{"unrelated auto-detect", `"auto_detect_interface":true`, `,"inet6_bind_address":"2001:db8::5"`, ""},
		{"explicit unspecified IPv4 still suppresses default", `"default_interface":"lan0"`, `,"inet4_bind_address":"0.0.0.0"`, ""},
		{"inactive defaults", `"default_interface":"","auto_detect_interface":false`, "", ""},
		{"inherited fixed default still unqualified", `"default_interface":"lan0"`, "", "inherits route.default_interface"},
		{"inherited auto-detect still unqualified", `"auto_detect_interface":true`, `,"inet4_bind_address":null`, "inherits route.auto_detect_interface"},
		{"invalid default", `"default_interface":5`, `,"bind_interface":"wan0"`, "invalid route.default_interface"},
		{"mark still rejected", `"default_mark":17`, `,"bind_interface":"wan0"`, "conflicting Core route control default_mark"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := []byte(`{"route":{` + tc.route + `},"outbounds":[{"type":"direct","tag":"selected"` + tc.dial + `},{"type":"direct","tag":"control"}]}`)
			err := validateZapretCoreProfile(config, true, map[string]bool{"selected": true})
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, entityoutbounds.ErrZapretPacketPathUnqualified) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestZapretInterfaceDefaultsRequireEverySelectedOutbound(t *testing.T) {
	config := []byte(`{"route":{"default_interface":"lan0"},"outbounds":[{"type":"direct","tag":"selected","bind_interface":"wan0"}]}`)
	err := validateZapretCoreProfile(config, true, map[string]bool{"selected": true, "missing": true})
	if !errors.Is(err, entityoutbounds.ErrZapretPacketPathUnqualified) {
		t.Fatalf("got %v, want an unqualified path", err)
	}
}

func TestZapretCoreProfileOnlyIgnoresDecodedZeroMarks(t *testing.T) {
	for _, tc := range []struct {
		mark string
		want bool
	}{
		{`0`, true}, {`"0x00"`, true}, {`"0b_0"`, true},
		{`1`, false}, {`"0xAB"`, false}, {`null`, false}, {`""`, false}, {`false`, false}, {`4294967296`, false},
	} {
		t.Run(tc.mark, func(t *testing.T) {
			for _, config := range []string{
				`{"route":{"default_mark":` + tc.mark + `}}`,
				`{"outbounds":[{"tag":"selected","routing_mark":` + tc.mark + `}]}`,
				`{"outbounds":[{"tag":"unselected","routing_mark":` + tc.mark + `}]}`,
				`{"endpoints":[{"tag":"endpoint","routing_mark":` + tc.mark + `}]}`,
			} {
				err := validateZapretCoreProfile([]byte(config), true, map[string]bool{"selected": true})
				if tc.want && err != nil || !tc.want && !errors.Is(err, entityoutbounds.ErrZapretPacketPathUnqualified) {
					t.Errorf("%s: got %v, accept = %t", config, err, tc.want)
				}
			}
		})
	}
}
