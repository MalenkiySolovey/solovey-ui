package box

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	sb "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/common/srs"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/domain"
)

func TestAnyTLSClientMetadataDefaultsAndRoundTrip(t *testing.T) {
	ctx := sb.Context(context.Background(), registry.InboundRegistry(), registry.OutboundRegistry(), registry.EndpointRegistry(), registry.DNSTransportRegistry(), registry.ServiceRegistry(), registry.CertificateProviderRegistry())
	for _, metadata := range []string{"", "custom-client"} {
		payload := map[string]any{"type": "anytls", "tag": "proxy", "server": "example.invalid", "server_port": 443, "tls": map[string]any{"enabled": true}}
		if metadata != "" {
			payload["client_metadata"] = metadata
		}
		raw, err := json.Marshal(map[string]any{"outbounds": []any{payload}})
		if err != nil {
			t.Fatal(err)
		}
		var options option.Options
		if err := options.UnmarshalJSONContext(ctx, raw); err != nil {
			t.Fatal(err)
		}
		typed, ok := options.Outbounds[0].Options.(*option.AnyTLSOutboundOptions)
		if !ok || typed.ClientMetadata != metadata {
			t.Fatalf("client metadata was changed by option decode: %#v", typed)
		}
		encoded, err := json.Marshal(typed)
		if err != nil {
			t.Fatal(err)
		}
		var restored option.AnyTLSOutboundOptions
		if err := json.Unmarshal(encoded, &restored); err != nil || restored.ClientMetadata != metadata {
			t.Fatalf("client metadata roundtrip: value=%q error=%v", restored.ClientMetadata, err)
		}
	}
}

func TestBinaryRuleSetAndDomainMatcherInputCompatibility(t *testing.T) {
	ruleSet := option.PlainRuleSet{Rules: []option.HeadlessRule{{Type: C.RuleTypeDefault, DefaultOptions: option.DefaultHeadlessRule{Domain: []string{"exact.example"}, DomainSuffix: []string{"suffix.example"}}}}}
	var compiled bytes.Buffer
	if err := srs.Write(&compiled, ruleSet, C.RuleSetVersionCurrent); err != nil {
		t.Fatal(err)
	}
	restored, err := srs.Read(bytes.NewReader(compiled.Bytes()), true)
	if err != nil || len(restored.Options.Rules) != 1 {
		t.Fatalf("valid binary rule set: %v", err)
	}
	decoded := restored.Options.Rules[0].DefaultOptions
	if !reflect.DeepEqual(decoded.Domain, ruleSet.Rules[0].DefaultOptions.Domain) || !reflect.DeepEqual(decoded.DomainSuffix, ruleSet.Rules[0].DefaultOptions.DomainSuffix) {
		t.Fatalf("binary rule set changed domain semantics: %#v", decoded)
	}
	for _, invalid := range [][]byte{{}, {'S', 'R'}, {'B', 'A', 'D', 0}, {'S', 'R', 'S', C.RuleSetVersionCurrent}} {
		if _, err := srs.Read(bytes.NewReader(invalid), true); err == nil {
			t.Fatalf("accepted malformed binary rule set %x", invalid)
		}
	}
	matcher := domain.NewMatcher([]string{"exact.example"}, []string{"suffix.example"}, false)
	var encoded bytes.Buffer
	if err := matcher.Write(&encoded); err != nil {
		t.Fatal(err)
	}
	copy, err := domain.ReadMatcher(bytes.NewReader(encoded.Bytes()))
	if err != nil || !copy.Match("exact.example") || !copy.Match("child.suffix.example") || copy.Match("unrelated.example") {
		t.Fatalf("domain matcher roundtrip changed matching: %v", err)
	}
	for length := 0; length < encoded.Len(); length++ {
		if _, err := domain.ReadMatcher(bytes.NewReader(encoded.Bytes()[:length])); err == nil {
			t.Fatalf("accepted truncated domain matcher length=%d", length)
		}
	}
	// The inner declared slice length must be consumed incrementally. A tiny
	// input with a maximal count must return an error without preallocating it.
	hugeCount := binary.AppendUvarint([]byte{0}, ^uint64(0))
	for _, invalid := range [][]byte{{0, 0, 0, 0}, hugeCount} {
		if _, err := domain.ReadMatcher(bytes.NewReader(invalid)); err == nil {
			t.Fatalf("accepted malformed domain matcher %x", invalid)
		}
	}
}
