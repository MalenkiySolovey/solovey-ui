package clientidentity

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveRightToLeftTrustedProxyChainAndCanonicalizesMappedIPv4(t *testing.T) {
	request := httptest.NewRequest("GET", "http://panel.example/api", nil)
	request.RemoteAddr = "10.0.0.2:443"
	request.Host = "Panel.Example:80"
	request.Header.Set("X-Forwarded-For", "198.51.100.7, ::ffff:10.0.0.1")
	request.Header.Set("X-Forwarded-Proto", "https")
	identity := Resolve(request, ParseConfig("10.0.0.0/8"))
	if identity.ClientIP != "198.51.100.7" ||
		identity.ClientPrefix != "198.51.100.0/24" ||
		identity.TrustedProxyHops != 2 ||
		identity.Provenance != ProvenanceTrustedXFF {
		t.Fatalf("unexpected client identity: %#v", identity)
	}
	if identity.ActualScheme != "http" || identity.DesiredScheme != "https" ||
		identity.SchemeSource != "trusted_x_forwarded_proto" ||
		identity.ExternalHost != "panel.example:80" {
		t.Fatalf("unexpected external transport identity: %#v", identity)
	}
}

func TestResolveIgnoresSpoofedForwardingFromUntrustedPeer(t *testing.T) {
	request := httptest.NewRequest("GET", "http://panel.example/api", nil)
	request.RemoteAddr = "203.0.113.9:1234"
	request.Header.Set("X-Forwarded-For", "198.51.100.7")
	request.Header.Set("X-Forwarded-Proto", "https")
	identity := Resolve(request, ParseConfig("10.0.0.0/8"))
	if identity.ClientIP != "203.0.113.9" || identity.DesiredScheme != "http" ||
		identity.Provenance != ProvenanceDirect {
		t.Fatalf("untrusted forwarding affected identity: %#v", identity)
	}
}

func TestBindingRevisionTracksProxyClientAndConfigurationAuthority(t *testing.T) {
	request := httptest.NewRequest("GET", "https://panel.example/api", nil)
	request.RemoteAddr = "10.0.0.2:443"
	request.Header.Set("X-Forwarded-For", "198.51.100.7")
	request.Header.Set("X-Forwarded-Proto", "https")
	base := Resolve(request, ParseConfig("10.0.0.2/32"))
	baseRevision := BindingRevision(base)
	if len(baseRevision) != 64 {
		t.Fatalf("binding revision length=%d", len(baseRevision))
	}

	changedClient := base
	changedClient.ClientIP = "198.51.100.8"
	changedClient.ClientPrefix = PrivacyPrefix(changedClient.ClientIP)
	changedConfig := base
	changedConfig.ConfigRevision = ParseConfig("10.0.0.0/24").Revision
	changedPath := base
	changedPath.Provenance = ProvenanceDirect
	for name, identity := range map[string]V1{
		"client":        changedClient,
		"configuration": changedConfig,
		"path":          changedPath,
	} {
		t.Run(name, func(t *testing.T) {
			if BindingRevision(identity) == baseRevision {
				t.Fatal("authority change did not invalidate binding revision")
			}
		})
	}
}

func TestResolveAllTrustedOrMissingForwardingIsUnknown(t *testing.T) {
	for name, forwardedFor := range map[string]string{
		"missing":     "",
		"all trusted": "10.0.0.4, 10.0.0.3",
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "http://panel.example/api", nil)
			request.RemoteAddr = "10.0.0.2:443"
			request.Header.Set("X-Forwarded-For", forwardedFor)
			identity := Resolve(request, ParseConfig("10.0.0.0/8"))
			if identity.Provenance != ProvenanceUnknown ||
				identity.ClientIP != "10.0.0.2" ||
				identity.ClientPrefix != "10.0.0.0/24" {
				t.Fatalf("ambiguous trusted chain yielded authority: %#v", identity)
			}
		})
	}
}

func TestResolveRejectsOverlongChainsAndAmbiguousForwardedProto(t *testing.T) {
	request := httptest.NewRequest("GET", "http://panel.example/api", nil)
	request.RemoteAddr = "10.0.0.2:443"
	hops := make([]string, MaxForwardedHops+1)
	for i := range hops {
		hops[i] = fmt.Sprintf("198.51.100.%d", (i%200)+1)
	}
	request.Header.Set("X-Forwarded-For", strings.Join(hops, ","))
	request.Header.Set("X-Forwarded-Proto", "https,http")
	identity := Resolve(request, ParseConfig("10.0.0.0/8"))
	if identity.ForwardedValid || identity.ClientIP != "10.0.0.2" ||
		identity.DesiredScheme != "http" {
		t.Fatalf("overlong/ambiguous forwarding was trusted: %#v", identity)
	}
}

func TestResolveRejectsDuplicateForwardedProtoHeaders(t *testing.T) {
	request := httptest.NewRequest("GET", "http://panel.example/api", nil)
	request.RemoteAddr = "10.0.0.2:443"
	request.Header.Add("X-Forwarded-Proto", "https")
	request.Header.Add("X-Forwarded-Proto", "http")
	identity := Resolve(request, ParseConfig("10.0.0.0/8"))
	if identity.ForwardedValid || identity.DesiredScheme != "http" ||
		identity.SchemeSource != "transport" {
		t.Fatalf("duplicate forwarded proto established authority: %#v", identity)
	}
}

func TestParseConfigCapsTrustedProxyEntries(t *testing.T) {
	entries := make([]string, MaxTrustedProxyCIDRs+20)
	for i := range entries {
		entries[i] = fmt.Sprintf("10.%d.0.0/16", i)
	}
	config := ParseConfig(strings.Join(entries, ","))
	if len(config.TrustedProxies) != MaxTrustedProxyCIDRs {
		t.Fatalf("trusted proxies=%d, want cap %d", len(config.TrustedProxies), MaxTrustedProxyCIDRs)
	}
	if !containsString(config.Warnings, "trusted_proxy_limit_exceeded") {
		t.Fatalf("missing proxy-limit warning: %#v", config.Warnings)
	}
}

func TestParseConfigExposesCanonicalActualSourceAndBroadWarnings(t *testing.T) {
	config := ParseConfig("127.0.0.0/8, 10.0.0.0/8, 198.51.100.10, invalid")
	if config.Source != "environment" {
		t.Fatalf("config source=%q", config.Source)
	}
	if got := strings.Join(config.CanonicalCIDRs, ","); got != "127.0.0.0/8,10.0.0.0/8,198.51.100.10/32" {
		t.Fatalf("canonical proxy config=%q", got)
	}
	for _, warning := range []string{
		"broad_private_trusted_proxy_cidr",
		"public_trusted_proxy",
		"invalid_trusted_proxy_entry",
	} {
		if !containsString(config.Warnings, warning) {
			t.Errorf("missing warning %q in %#v", warning, config.Warnings)
		}
	}
	changed := ParseConfig("127.0.0.0/8, 10.0.0.0/8, 198.51.100.10")
	if changed.Revision == config.Revision {
		t.Fatal("invalid-entry configuration change did not invalidate revision")
	}
}

func TestParseConfigPreservesIPv4MappedPrefixWidth(t *testing.T) {
	config := ParseConfig("::ffff:192.0.2.0/120")
	if len(config.CanonicalCIDRs) != 1 || config.CanonicalCIDRs[0] != "192.0.2.0/24" {
		t.Fatalf("mapped prefix canonicalization=%#v, want IPv4 /24", config.CanonicalCIDRs)
	}
	if !contains(config.TrustedProxies, "192.0.2.42") || contains(config.TrustedProxies, "192.0.3.1") {
		t.Fatalf("mapped prefix trust width is incorrect: %#v", config.TrustedProxies)
	}
	invalid := ParseConfig("::ffff:192.0.2.0/80")
	if len(invalid.TrustedProxies) != 0 || !containsString(invalid.Warnings, "invalid_trusted_proxy_entry") {
		t.Fatalf("over-broad mapped prefix was not rejected: %#v", invalid)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestCanonicalHostPortRejectsAmbiguityAndNormalizesValidAuthority(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{value: "Panel.Example.:08443", want: "panel.example:8443"},
		{value: "[2001:db8::1]", want: "[2001:db8::1]"},
		{value: "[2001:db8::1]:443", want: "[2001:db8::1]:443"},
		{value: "panel.example,evil.example"},
		{value: "panel.example evil.example"},
		{value: "user@panel.example"},
		{value: "panel.example:0"},
		{value: "panel.example:65536"},
		{value: "2001:db8::1"},
		{value: "-panel.example"},
		{value: "panel_.example"},
	}
	for _, test := range tests {
		if got := CanonicalHostPort(test.value); got != test.want {
			t.Errorf("CanonicalHostPort(%q)=%q, want %q", test.value, got, test.want)
		}
	}
}

func BenchmarkResolveBoundedProxyChain(b *testing.B) {
	request := httptest.NewRequest("GET", "http://panel.example/api", nil)
	request.RemoteAddr = "10.0.0.2:443"
	request.Header.Set("X-Forwarded-For", "198.51.100.7, 10.0.0.4, 10.0.0.3")
	request.Header.Set("X-Forwarded-Proto", "https")
	config := ParseConfig("10.0.0.0/8")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Resolve(request, config)
	}
}
