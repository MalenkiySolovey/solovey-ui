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
		if reason, unavailable := TUNDNSUnavailableModes()[mode]; unavailable {
			findings = append(findings, diagnostics.Finding{Kind: "inbound", Path: fmt.Sprintf("inbounds[%d].dns_mode", i), Code: reason, Severity: diagnostics.Error, Message: "This DNS mode needs an injected interface-DNS runtime authorization contract that the product does not currently provide. Choose disabled or preserve the old configuration for manual correction.", MigrationOutcome: diagnostics.ManualRequired, OperatorActionRequired: true})
		}
	}
	return findings
}

// Kernel capability or an advanced profile alone cannot grant system-DNS
// mutation. No current deployment/broker contract authorizes this new effect.
// Keep the negative product fact here until such a contract is implemented.
func TUNDNSUnavailableModes() map[string]string {
	return map[string]string{"native": "tun_dns_capability_unavailable", "hijack": "tun_dns_capability_unavailable"}
}
