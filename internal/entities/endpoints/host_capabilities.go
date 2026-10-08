package endpoints

import (
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/sagernet/sing-box/option"
)

func HostCapabilityFindings(kind, path string, source json.RawMessage) []diagnostics.Finding {
	if kind != "tailscale" {
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(source, &fields) != nil {
		return nil
	}
	result := []diagnostics.Finding{}
	fail := func(key, code string) {
		result = append(result, diagnostics.Finding{Kind: "endpoint", Path: path + "." + key, Code: code, Severity: diagnostics.Error, Message: "Tailscale host SSH and file-drop activation have no product/deployment authorization. Use a valid disabled shape for the named host feature before retrying.", MigrationOutcome: diagnostics.UnsupportedLegacy, OperatorActionRequired: true})
	}
	if raw, present := fields["ssh_server"]; present {
		var options option.TailscaleSSHServerOptions
		if json.Unmarshal(raw, &options) != nil {
			fail("ssh_server", "TAILSCALE_HOST_OPTIONS_INVALID")
		} else if options.Enabled {
			fail("ssh_server", "TAILSCALE_HOST_FEATURE_UNAVAILABLE")
		}
	}
	var directory string
	if raw, present := fields["taildrop_directory"]; present {
		if json.Unmarshal(raw, &directory) != nil {
			fail("taildrop_directory", "TAILSCALE_HOST_OPTIONS_INVALID")
		} else if directory != "" {
			fail("taildrop_directory", "TAILSCALE_HOST_FEATURE_UNAVAILABLE")
		}
	}
	return result
}

func HostConfigFindings(source []byte) []diagnostics.Finding {
	var root struct {
		Endpoints []json.RawMessage `json:"endpoints"`
	}
	if json.Unmarshal(source, &root) != nil {
		return nil
	}
	result := []diagnostics.Finding{}
	for i, raw := range root.Endpoints {
		var header struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(raw, &header)
		result = append(result, HostCapabilityFindings(header.Type, fmt.Sprintf("endpoints[%d]", i), raw)...)
	}
	return result
}
