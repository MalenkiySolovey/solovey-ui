package realtimehttp

import (
	"net/http/httptest"
	"testing"

	clientidentity "github.com/MalenkiySolovey/solovey-ui/internal/httpsecurity/clientidentity"
)

func TestOriginAllowedV1DirectAuthorityMatrix(t *testing.T) {
	identity := clientidentity.V1{
		DesiredScheme:  "http",
		ExternalHost:   "panel.example:8443",
		ForwardedValid: true,
	}
	tests := []struct {
		name   string
		origin string
		want   bool
		reason string
	}{
		{name: "same non-default authority", origin: "http://panel.example:8443", want: true, reason: "external_origin"},
		{name: "foreign host", origin: "http://other.example:8443", reason: "host_mismatch"},
		{name: "foreign port", origin: "http://panel.example:2096", reason: "host_mismatch"},
		{name: "foreign scheme", origin: "https://panel.example:8443", reason: "scheme_mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, reason := OriginAllowedV1(test.origin, identity, "")
			if got != test.want || reason != test.reason {
				t.Fatalf("OriginAllowedV1()=(%v,%q), want (%v,%q)", got, reason, test.want, test.reason)
			}
		})
	}
}

func TestOriginAllowedV1NormalizesDefaultPort(t *testing.T) {
	identity := clientidentity.V1{
		DesiredScheme:  "https",
		ExternalHost:   "panel.example:443",
		ForwardedValid: true,
	}
	if allowed, reason := OriginAllowedV1("https://panel.example", identity, ""); !allowed || reason != "external_origin" {
		t.Fatalf("default-port origin rejected: allowed=%v reason=%q", allowed, reason)
	}
}

func TestOriginAllowedV1UsesOnlyTrustedProxyFacts(t *testing.T) {
	trustedRequest := httptest.NewRequest("GET", "http://panel.example:443/api", nil)
	trustedRequest.RemoteAddr = "10.0.0.2:1234"
	trustedRequest.Header.Set("X-Forwarded-For", "198.51.100.7")
	trustedRequest.Header.Set("X-Forwarded-Proto", "https")
	trustedRequest.Header.Set("X-Forwarded-Host", "attacker.example")
	trusted := clientidentity.Resolve(trustedRequest, clientidentity.ParseConfig("10.0.0.0/8"))
	if allowed, reason := OriginAllowedV1("https://panel.example", trusted, ""); !allowed || reason != "external_origin" {
		t.Fatalf("trusted proxy same-origin rejected: allowed=%v reason=%q identity=%#v", allowed, reason, trusted)
	}

	untrustedRequest := httptest.NewRequest("GET", "http://panel.example:8443/api", nil)
	untrustedRequest.RemoteAddr = "198.51.100.7:1234"
	untrustedRequest.Header.Set("X-Forwarded-Proto", "https")
	untrustedRequest.Header.Set("X-Forwarded-Host", "attacker.example")
	untrusted := clientidentity.Resolve(untrustedRequest, clientidentity.ParseConfig("10.0.0.0/8"))
	if allowed, reason := OriginAllowedV1("http://panel.example:8443", untrusted, ""); !allowed || reason != "external_origin" {
		t.Fatalf("untrusted forwarded headers changed direct authority: allowed=%v reason=%q identity=%#v", allowed, reason, untrusted)
	}
	if allowed, reason := OriginAllowedV1("https://panel.example:8443", untrusted, ""); allowed || reason != "scheme_mismatch" {
		t.Fatalf("untrusted forwarded proto was accepted: allowed=%v reason=%q identity=%#v", allowed, reason, untrusted)
	}
}
