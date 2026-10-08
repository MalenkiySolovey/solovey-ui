package entitytls

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"gorm.io/gorm"
)

// PrepareOptionsUpgrade removes only the ECH fields proven inert in both
// accepted core versions. It preserves every other TLS credential and choice.
func PrepareOptionsUpgrade(path string, source json.RawMessage) (json.RawMessage, []diagnostics.Finding, error) {
	if len(bytes.TrimSpace(source)) == 0 || bytes.Equal(bytes.TrimSpace(source), []byte("null")) {
		return source, nil, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(source, &fields) != nil || fields == nil {
		return source, nil, fmt.Errorf("TLS options must be an object")
	}
	var ech map[string]json.RawMessage
	if json.Unmarshal(fields["ech"], &ech) != nil {
		return source, nil, nil
	}
	findings := []diagnostics.Finding{}
	for _, key := range []string{"pq_signature_schemes_enabled", "dynamic_record_sizing_disabled"} {
		if raw, present := ech[key]; present {
			var value bool
			if json.Unmarshal(raw, &value) != nil {
				return source, findings, fmt.Errorf("ECH compatibility flag must be boolean")
			}
			delete(ech, key)
			findings = append(findings, providerFinding(path+".ech."+key, "TLS_ECH_NOOP_REMOVED", diagnostics.Warn, "The accepted core treats this ECH compatibility flag as a no-op. Its removal preserves ECH keys and behavior."))
		}
	}
	if len(findings) == 0 {
		return source, findings, nil
	}
	fields["ech"], _ = json.Marshal(ech)
	candidate, err := json.Marshal(fields)
	return candidate, findings, err
}

// StageStoredUpgrade changes only the caller's unpublished migration view.
// Provider extraction and envelope lifecycle remain owned by this package.
func StageStoredUpgrade(tx *gorm.DB) ([]diagnostics.Finding, error) {
	definitions, err := ReadProviderDefinitions(tx)
	if err != nil {
		return nil, err
	}
	var profiles []model.Tls
	if tx.Migrator().HasTable(&model.Tls{}) {
		if err := tx.Order("id").Find(&profiles).Error; err != nil {
			return nil, err
		}
	}
	findings := []diagnostics.Finding{}
	for i := range profiles {
		for _, side := range []struct {
			name string
			raw  *json.RawMessage
		}{{"server", &profiles[i].Server}, {"client", &profiles[i].Client}} {
			candidate, local, err := PrepareOptionsUpgrade(fmt.Sprintf("tls[%d].%s", profiles[i].Id, side.name), *side.raw)
			findings = append(findings, local...)
			if err != nil {
				return findings, err
			}
			*side.raw = candidate
		}
	}
	prepared, err := PrepareProviderUpgrade(profiles, definitions)
	findings = append(findings, prepared.Findings...)
	if err != nil {
		return findings, err
	}
	if err := tx.AutoMigrate(&model.TLSCertificateProvider{}); err != nil {
		return findings, err
	}
	if err := WriteProviderDefinitions(tx, prepared.Providers); err != nil {
		return findings, err
	}
	for _, row := range prepared.Profiles {
		if err := tx.Model(&model.Tls{}).Where("id = ?", row.Id).Updates(map[string]any{"server": row.Server, "client": row.Client}).Error; err != nil {
			return findings, err
		}
	}
	return findings, nil
}

func CompatibilityCatalogue() []diagnostics.CompatibilityFact {
	return []diagnostics.CompatibilityFact{
		{"DEP-09", "tls.acme/certificate_provider; certificate_providers", "DEPRECATED_ACCEPTED", "One encrypted TLS-owned provider store, deterministic identity and legacy inline runtime semantics; conflicts are manual."},
		{"DEP-11", "tls.ech.pq_signature_schemes_enabled/dynamic_record_sizing_disabled", "NO_OP", "Remove proven inert boolean flags; retain active ECH keys and config."},
	}
}
