package entityinbounds

import (
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
)

// TUNDNSFindings belongs to the inbound owner. A missing old dns_mode cannot
// authorize 1.14's new interface-DNS mutation. Until an equivalent deployment
// witness exists, explicit operator selection is required; no OS operation is
// performed by inspection, construction, or migration.
func TUNDNSFindings(config []byte) []diagnostics.Finding {
	var root map[string]json.RawMessage
	_ = json.Unmarshal(config, &root)
	var rows []map[string]json.RawMessage
	_ = json.Unmarshal(root["inbounds"], &rows)
	var findings []diagnostics.Finding
	for i, row := range rows {
		var kind, mode string
		_ = json.Unmarshal(row["type"], &kind)
		if kind != "tun" {
			continue
		}
		_ = json.Unmarshal(row["dns_mode"], &mode)
		if mode == "" {
			findings = append(findings, diagnostics.Finding{Kind: "inbound", Path: fmt.Sprintf("inbounds[%d].dns_mode", i), Code: "tun_dns_mode_manual", Severity: diagnostics.Error, Message: "Select disabled, hijack, or native DNS explicitly for this TUN. The new default changes interface DNS; existing deployment authorization still applies.", MigrationOutcome: diagnostics.ManualRequired, OperatorActionRequired: true})
		}
	}
	return findings
}
