package canonical

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func irFixture() map[string]any {
	return map[string]any{"type": "vless", "tag": "Public label", "server": "example.com", "server_port": 443, "uuid": "11111111-1111-4111-8111-111111111111", "tls": map[string]any{"enabled": true, "server_name": "tls.example", "alpn": []string{"h2", "http/1.1"}}, "transport": map[string]any{"type": "ws", "path": "/edge", "headers": map[string]any{"Host": "edge.example"}}}
}

func TestEndpointIRIdentityUsesSemanticsAndRetainsProvenance(t *testing.T) {
	first := irFixture()
	second := cloneMap(first)
	second["tag"] = "Renamed display label"
	second["server_port"] = "443"
	a := ObserveOutbound(FormatSingBox, first, 0)
	b := ObserveOutbound(FormatURI, second, 0)
	if a.EndpointID == "" || a.EndpointID != b.EndpointID {
		t.Fatal("label, format or numeric port carrier changed endpoint identity")
	}
	second["uuid"] = "22222222-2222-4222-8222-222222222222"
	c := ObserveOutbound(FormatURI, second, 0)
	if c.EndpointID == a.EndpointID {
		t.Fatal("changed authentication retained endpoint identity")
	}
	if a.Observations[0].Format != FormatSingBox || b.Observations[0].Format != FormatURI {
		t.Fatal("actual source format provenance missing")
	}
	projected, err := CheckedEndpointOutbound(a)
	if err != nil || !reflect.DeepEqual(CleanOutbound(first), projected) {
		t.Fatal("IR changed exact native endpoint semantics")
	}
}

func TestEndpointIRDisabledTLSFeaturesRetainSourceWithoutChangingActiveIdentity(t *testing.T) {
	for _, feature := range []string{"utls", "reality"} {
		t.Run(feature, func(t *testing.T) {
			original := irFixture()
			passive := cloneMap(original)
			options := map[string]any{"enabled": false}
			if feature == "utls" {
				options["fingerprint"] = "chrome"
			} else {
				options["public_key"] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
				options["short_id"] = "aabb"
			}
			passive["tls"].(map[string]any)[feature] = options
			before := ObserveOutbound(FormatSingBox, original, 0)
			after := ObserveOutbound(FormatSingBox, passive, 0)
			if before.EndpointID != after.EndpointID {
				t.Fatal("passive TLS settings changed active endpoint identity")
			}
			if !reflect.DeepEqual(after.BestOutbound["tls"].(map[string]any)[feature], options) ||
				!reflect.DeepEqual(after.Observations[0].Outbound["tls"].(map[string]any)[feature], options) {
				t.Fatal("inactive source configuration disappeared")
			}
			options["enabled"] = true
			active := ObserveOutbound(FormatSingBox, passive, 0)
			if active.EndpointID == after.EndpointID {
				t.Fatal("enabling TLS feature retained inactive identity")
			}
		})
	}
}

func TestEndpointIRConflictRetainsBothObservationsAndBlocksActivation(t *testing.T) {
	for _, field := range []string{"uuid", "tls", "transport", "type"} {
		t.Run(field, func(t *testing.T) {
			first, second := irFixture(), irFixture()
			switch field {
			case "uuid":
				second[field] = "22222222-2222-4222-8222-222222222222"
			case "tls":
				second[field] = map[string]any{"enabled": true, "insecure": true}
			case "transport":
				second[field] = map[string]any{"type": "grpc", "service_name": "different"}
			case "type":
				second[field] = "vmess"
			}
			merged := MergeSnapshots(ObserveOutbounds(FormatSingBox, []map[string]any{first}), ObserveOutbounds(FormatURI, []map[string]any{second}))
			if len(merged.Connections) != 1 {
				t.Fatal("current source label matching changed")
			}
			endpoint := merged.Connections[0]
			if endpoint.Projection != ProjectionManual || len(endpoint.Observations) != 2 {
				t.Fatal("critical disagreement was silently selected")
			}
			if _, err := CheckedEndpointOutbound(endpoint); err == nil {
				t.Fatal("conflicting endpoint became active")
			}
			encoded, _ := json.Marshal(endpoint.Diagnostics)
			if strings.Contains(string(encoded), "22222222") || strings.Contains(string(encoded), "different") {
				t.Fatal("diagnostics exposed source values")
			}
		})
	}
}

func TestEndpointIRPreservedOptionsDoNotAliasParserInput(t *testing.T) {
	raw := irFixture()
	raw["extension"] = map[string]any{"values": []string{"retained"}}
	native := map[string]any{"critical-future-option": map[string]any{"mode": "preserved"}}
	CaptureSource(raw, FormatXray, native, ProjectionDiagnostic{State: ProjectionManual, Code: "source_fields_unmapped", Field: "endpoint"})
	endpoint := ObserveOutbound(FormatXray, raw, 0)
	raw["tls"].(map[string]any)["alpn"].([]string)[0] = "changed"
	raw["extension"].(map[string]any)["values"].([]string)[0] = "changed"
	native["critical-future-option"].(map[string]any)["mode"] = "changed"
	if endpoint.TLS.Options["alpn"].([]string)[0] != "h2" || endpoint.BestOutbound["extension"].(map[string]any)["values"].([]string)[0] != "retained" {
		t.Fatal("IR aliases mutable parser options")
	}
	if endpoint.Observations[0].Source["options"].(map[string]any)["critical-future-option"].(map[string]any)["mode"] != "preserved" {
		t.Fatal("native unsupported source was mutated")
	}
	if _, err := CheckedEndpointOutbound(endpoint); err == nil {
		t.Fatal("unmapped source critical field became active")
	}
}
