package validation

import (
	"encoding/json"
	"fmt"

	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	entityendpoints "github.com/MalenkiySolovey/solovey-ui/internal/entities/endpoints"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
)

// OwnerOptionsFindings orders owner facts, without interpreting their fields.
// Save, Doctor, import, migration and the pinned dry build share this result.
func OwnerOptionsFindings(source []byte) []diagnostics.Finding {
	result := singboxconfig.BaseOptionsFindings(source)
	result = append(result, entityendpoints.HostConfigFindings(source)...)
	var root map[string]json.RawMessage
	if json.Unmarshal(source, &root) != nil {
		return result
	}
	for _, section := range []string{"inbounds", "outbounds", "endpoints", "services"} {
		var rows []json.RawMessage
		if json.Unmarshal(root[section], &rows) != nil {
			continue
		}
		for i, raw := range rows {
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil {
				continue
			}
			var kind string
			_ = json.Unmarshal(fields["type"], &kind)
			path := fmt.Sprintf("%s[%d]", section, i)
			fact := entitycapabilities.Resolve(section, kind)
			if fact.Known && !fact.SupportedByProduct {
				result = append(result, diagnostics.Finding{Kind: "capability", Path: path + ".type", Code: "PRODUCT_CAPABILITY_UNAVAILABLE", Severity: diagnostics.Error, Message: "This entity has no product authorization. Registration in the core does not enable it.", MigrationOutcome: diagnostics.UnsupportedLegacy, OperatorActionRequired: true})
			}
			if section == "inbounds" {
				result = append(result, entityinbounds.LegacyOptionsFindings(kind, path, raw)...)
			}
			if section == "outbounds" {
				_, local, _ := entityoutbounds.PrepareOptionsUpgrade(kind, path, raw)
				result = append(result, local...)
			}
			if tls, present := fields["tls"]; present {
				_, local, _ := entitytls.PrepareOptionsUpgrade(path+".tls", tls)
				result = append(result, local...)
			}
		}
	}
	return result
}
