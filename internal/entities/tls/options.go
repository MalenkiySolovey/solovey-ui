package entitytls

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/sagernet/sing-box/option"
	sbjson "github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/common/json/badoption"
	"slices"
)

var clientAuthenticationModes = []string{"no", "request", "require-any", "verify-if-given", "require-and-verify"}

// TLSOptionsFindings is shared by stored/editor/save and complete runtime
// validation. File loading belongs to the certificate/runtime owners.
func TLSOptionsFindings(side, path string, source json.RawMessage) []diagnostics.Finding {
	var fields map[string]json.RawMessage
	if json.Unmarshal(source, &fields) != nil || fields == nil {
		return []diagnostics.Finding{providerFinding(path, "TLS_OPTIONS_INVALID", diagnostics.Error, "TLS options must be an object.")}
	}
	findings := []diagnostics.Finding{}
	fail := func(field, code, message string) {
		findings = append(findings, providerFinding(path+"."+field, code, diagnostics.Error, message))
	}
	var enabled bool
	_ = json.Unmarshal(fields["enabled"], &enabled)
	if raw, present := fields["engine"]; present {
		var engine string
		if json.Unmarshal(raw, &engine) != nil || engine != "" && engine != "go" {
			fail("engine", "TLS_ENGINE_UNAVAILABLE", "Only the product's Go TLS engine is authorized.")
		}
	}
	for _, key := range []string{"spoof", "spoof_method"} {
		if nonemptyTLSValue(fields[key]) {
			fail(key, "TLS_SPOOF_UNAVAILABLE", "Privileged TLS spoof activation has no product/deployment authorization.")
		}
	}
	if raw, present := fields["handshake_timeout"]; present {
		var duration badoption.Duration
		if json.Unmarshal(raw, &duration) != nil || duration < 0 {
			fail("handshake_timeout", "TLS_HANDSHAKE_TIMEOUT_INVALID", "Handshake timeout must be a nonnegative pinned duration.")
		}
	}
	if side == "server" {
		if raw, present := fields["client_authentication"]; present {
			var mode string
			_ = json.Unmarshal(raw, &mode)
			if !slices.Contains(clientAuthenticationModes, mode) {
				fail("client_authentication", "TLS_CLIENT_AUTH_MODE_INVALID", "Choose a supported server client-authentication mode.")
			}
		}
		if raw, present := fields["client_certificate_path"]; present {
			var paths badoption.Listable[string]
			if json.Unmarshal(raw, &paths) != nil {
				fail("client_certificate_path", "TLS_CLIENT_CA_PATH_INVALID", "Server client CA paths use the pinned listable string shape.")
			}
		}
	} else {
		for _, key := range []string{"client_certificate_path", "client_key_path"} {
			if raw, present := fields[key]; present {
				var value string
				if json.Unmarshal(raw, &value) != nil {
					fail(key, "TLS_CLIENT_PAIR_PATH_INVALID", "Client certificate and key paths are scalar strings.")
				}
			}
		}
	}
	pairs := [][2]string{{"certificate", "certificate_path"}}
	if side == "server" {
		pairs = append(pairs, [2]string{"key", "key_path"}, [2]string{"client_certificate", "client_certificate_path"})
	} else {
		pairs = append(pairs, [2]string{"client_certificate", "client_certificate_path"}, [2]string{"client_key", "client_key_path"})
	}
	for _, pair := range pairs {
		if nonemptyTLSValue(fields[pair[0]]) && nonemptyTLSValue(fields[pair[1]]) {
			fail(pair[0], "TLS_CREDENTIAL_MODE_CONFLICT", "Text and path credentials are both populated. Choose a mode explicitly before saving.")
		}
	}
	if enabled && side == "client" {
		if nonemptyTLSValue(fields["certificate_public_key_sha256"]) && (nonemptyTLSValue(fields["certificate"]) || nonemptyTLSValue(fields["certificate_path"])) {
			fail("certificate_public_key_sha256", "TLS_PIN_CA_CONFLICT", "The pinned Go client accepts a public-key pin or a CA certificate source. Choose one before retrying.")
		}
		certificate := nonemptyTLSValue(fields["client_certificate"]) || nonemptyTLSValue(fields["client_certificate_path"])
		key := nonemptyTLSValue(fields["client_key"]) || nonemptyTLSValue(fields["client_key_path"])
		if certificate != key {
			fail("client_certificate", "TLS_CLIENT_PAIR_INCOMPLETE", "A client certificate and its private key must be supplied together.")
		}
	}
	if enabled {
		if _, inlineACME := fields["acme"]; inlineACME && !registry.Resolve("certificateProviders", "acme").Available() {
			fail("acme", "TLS_PROVIDER_UNAVAILABLE", "ACME is not available in this build and product composition.")
		}
		var err error
		if side == "server" {
			var pinned option.InboundTLSOptions
			err = sbjson.UnmarshalContext(registry.Context(context.Background()), source, &pinned)
		} else {
			var pinned option.OutboundTLSOptions
			err = sbjson.UnmarshalContext(registry.Context(context.Background()), source, &pinned)
		}
		if err != nil {
			fail("", "TLS_OPTIONS_INVALID", "TLS options are rejected by the pinned consumer schema.")
		}
	}
	return findings
}

// CertificateConfigFindings validates direct core JSON at the same owner as
// assembly, so registered OriginCA/Tailscale constructors do not authorize a
// provider that lacks the TLS product contract.
func CertificateConfigFindings(source []byte) []diagnostics.Finding {
	projection, _ := ProjectCertificateProviders(source, nil)
	return projection.Findings
}

func nonemptyTLSValue(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return true
	}
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return v != ""
	case []any:
		return len(v) > 0
	default:
		return true
	}
}

func TLSConfigFindings(source []byte) []diagnostics.Finding {
	var root map[string]json.RawMessage
	if json.Unmarshal(source, &root) != nil {
		return nil
	}
	var result []diagnostics.Finding
	for _, section := range []string{"inbounds", "outbounds", "services", "http_clients"} {
		var values []map[string]json.RawMessage
		if json.Unmarshal(root[section], &values) != nil {
			continue
		}
		for i, value := range values {
			if raw, present := value["tls"]; present {
				side := "server"
				if section == "outbounds" || section == "http_clients" {
					side = "client"
				}
				result = append(result, TLSOptionsFindings(side, fmt.Sprintf("%s[%d].tls", section, i), raw)...)
			}
		}
	}
	return result
}
