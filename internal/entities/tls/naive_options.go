package entitytls

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/sagernet/sing-box/option"
	sbjson "github.com/sagernet/sing/common/json"
)

// NaiveOutboundTLSFindings validates explicit TLS policy before stored options
// can commit. The semantic baseline is official sing-box v1.14.2
// protocol/naive/outbound.go: its native client rejects several Go TLS options
// and does not consume SPKI pins or Go TLS handshake/fragment fallback timers.
// File reads and native client construction remain with their runtime owners.
func NaiveOutboundTLSFindings(path string, source json.RawMessage) []diagnostics.Finding {
	if raw := bytes.TrimSpace(source); len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return []diagnostics.Finding{providerFinding(path, "NAIVE_TLS_REQUIRED", diagnostics.Error, "Naive outbound requires enabled TLS.")}
	}
	findings := TLSOptionsFindings("client", path, source)
	var options option.OutboundTLSOptions
	if err := sbjson.UnmarshalContext(registry.Context(context.Background()), source, &options); err != nil {
		if len(findings) == 0 {
			return []diagnostics.Finding{providerFinding(path, "TLS_OPTIONS_INVALID", diagnostics.Error, "TLS options are rejected by the pinned consumer schema.")}
		}
		return findings
	}
	if !options.Enabled {
		return append(findings, providerFinding(path+".enabled", "NAIVE_TLS_REQUIRED", diagnostics.Error, "Naive outbound requires enabled TLS."))
	}
	reject := func(field string, active bool) {
		if active {
			findings = append(findings, providerFinding(path+"."+field, "NAIVE_TLS_OPTION_UNSUPPORTED", diagnostics.Error, "The pinned native Naive client does not support this TLS option. Choose supported Naive TLS policy before saving."))
		}
	}
	reject("disable_sni", options.DisableSNI)
	reject("insecure", options.Insecure)
	reject("alpn", len(options.ALPN) > 0)
	reject("min_version", options.MinVersion != "")
	reject("max_version", options.MaxVersion != "")
	reject("cipher_suites", len(options.CipherSuites) > 0)
	reject("curve_preferences", len(options.CurvePreferences) > 0)
	reject("client_certificate", len(options.ClientCertificate) > 0)
	reject("client_certificate_path", options.ClientCertificatePath != "")
	reject("client_key", len(options.ClientKey) > 0)
	reject("client_key_path", options.ClientKeyPath != "")
	reject("fragment", options.Fragment)
	reject("record_fragment", options.RecordFragment)
	reject("fragment_fallback_delay", options.FragmentFallbackDelay != 0)
	reject("kernel_tx", options.KernelTx)
	reject("kernel_rx", options.KernelRx)
	reject("handshake_timeout", options.HandshakeTimeout != 0)
	reject("utls", options.UTLS != nil && options.UTLS.Enabled)
	reject("reality", options.Reality != nil && options.Reality.Enabled)
	if len(options.CertificatePublicKeySHA256) > 0 {
		findings = append(findings, providerFinding(path+".certificate_public_key_sha256", "NAIVE_PUBLIC_KEY_PIN_UNSUPPORTED", diagnostics.Error, "Naive cannot enforce SPKI pins. Supply a trusted CA certificate source before saving."))
	}
	return findings
}
