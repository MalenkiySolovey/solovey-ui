package entitytls

import (
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
)

// PublicExportFindings recognizes certificate material at its actual TLS
// consumers. It blocks the export rather than silently deleting credentials or
// changing the stored configuration. User protocol passwords remain owned by
// the subscription delivery contract, outside this certificate policy.
func PublicExportFindings(source []byte) []diagnostics.Finding {
	var root map[string]json.RawMessage
	if json.Unmarshal(source, &root) != nil {
		return nil
	}
	var findings []diagnostics.Finding
	if nonemptyTLSValue(root["certificate_providers"]) {
		findings = append(findings, providerFinding("certificate_providers", "TLS_PRIVATE_EXPORT_REQUIRED", diagnostics.Error, "Provider records belong to owner-approved private backup. Remove them from the public subscription template before retrying."))
	}
	check := func(raw json.RawMessage, path string) {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return
		}
		for _, key := range []string{"acme", "certificate_provider", "key", "key_path", "private_key", "client_key", "client_key_path", "certificate_path", "client_certificate_path"} {
			if nonemptyTLSValue(fields[key]) {
				findings = append(findings, providerFinding(path+"."+key, "TLS_PRIVATE_EXPORT_REQUIRED", diagnostics.Error, "Private certificate material or an internal file reference requires owner-approved private backup. Configure a public client profile before exporting."))
			}
		}
		for _, key := range []string{"reality", "ech"} {
			var nested map[string]json.RawMessage
			_ = json.Unmarshal(fields[key], &nested)
			for _, secret := range []string{"private_key", "key", "key_path"} {
				if nonemptyTLSValue(nested[secret]) {
					findings = append(findings, providerFinding(path+"."+key+"."+secret, "TLS_PRIVATE_EXPORT_REQUIRED", diagnostics.Error, "Private server certificate material is unavailable in a public export."))
				}
			}
		}
	}
	if raw, present := root["tls"]; present {
		check(raw, "tls")
	}
	for _, section := range []string{"inbounds", "outbounds", "services"} {
		var entities []map[string]json.RawMessage
		_ = json.Unmarshal(root[section], &entities)
		for index, entity := range entities {
			if raw, present := entity["tls"]; present {
				check(raw, fmt.Sprintf("%s[%d].tls", section, index))
			}
		}
	}
	return findings
}
