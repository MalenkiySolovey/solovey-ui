package entitytls

import (
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"gorm.io/gorm"
)

// GetAllViews keeps cleartext candidate material inside the TLS owner. Before
// the final migration, compatible inline profiles are presented through the
// same provider DTO without publishing a partial automatic upgrade.
func GetAllViews(db *gorm.DB) ([]model.Tls, error) {
	rows, err := GetAll(db)
	if err != nil {
		return nil, err
	}
	stored, err := ReadProviderDefinitions(db)
	if err != nil {
		return nil, err
	}
	for i, row := range rows {
		prepared, prepareErr := PrepareProviderUpgrade([]model.Tls{row}, stored)
		definitions := stored
		if prepareErr == nil {
			rows[i] = prepared.Profiles[0]
			definitions = prepared.Providers
		}
		var server map[string]json.RawMessage
		_ = json.Unmarshal(rows[i].Server, &server)
		var tag string
		_ = json.Unmarshal(server["certificate_provider"], &tag)
		if definition, found := findDefinition(definitions, tag); found {
			rows[i].Provider, _ = json.Marshal(ProviderView{Tag: tag, Type: definition.Type, RuntimeMode: definition.RuntimeMode, Options: MaskProviderOptions(definition.Options)})
		}
		if raw, present := server["acme"]; present {
			server["acme"] = MaskProviderOptions(raw)
			rows[i].Server, _ = json.Marshal(server)
		}
		findings := append([]diagnostics.Finding{}, prepared.Findings...)
		findings = append(findings, TLSOptionsFindings("server", fmt.Sprintf("tls[%d].server", row.Id), rows[i].Server)...)
		findings = append(findings, TLSOptionsFindings("client", fmt.Sprintf("tls[%d].client", row.Id), rows[i].Client)...)
		if len(findings) > 0 {
			rows[i].Compatibility, _ = json.Marshal(struct {
				Outcome  string                `json:"outcome"`
				Findings []diagnostics.Finding `json:"findings"`
				Blocked  bool                  `json:"blocked"`
			}{diagnostics.Outcome(findings), findings, diagnostics.FirstError(findings) != nil})
		}
	}
	return rows, nil
}

// sourceProvider resolves a displayed pre-migration provider from its actual
// stored TLS row. A UI marker or submitted identity is never a secret source.
func sourceProvider(tx *gorm.DB, tag string, stored []ProviderDefinition) (ProviderDefinition, bool, error) {
	if definition, found := findDefinition(stored, tag); found {
		return definition, true, nil
	}
	rows, err := GetAll(tx)
	if err != nil {
		return ProviderDefinition{}, false, err
	}
	for _, row := range rows {
		prepared, err := PrepareProviderUpgrade([]model.Tls{row}, stored)
		if err != nil {
			continue
		}
		if definition, found := findDefinition(prepared.Providers, tag); found {
			return definition, true, nil
		}
	}
	return ProviderDefinition{}, false, nil
}

func prepareProfileDraft(tx *gorm.DB, profile *model.Tls) ([]ProviderDefinition, error) {
	stored, err := ReadProviderDefinitions(tx)
	if err != nil {
		return nil, err
	}
	var server map[string]json.RawMessage
	_ = json.Unmarshal(profile.Server, &server)
	if raw, present := server["acme"]; present {
		var old model.Tls
		if profile.Id > 0 {
			if err := tx.Where("id = ?", profile.Id).Take(&old).Error; err != nil {
				return nil, err
			}
		}
		var previous map[string]json.RawMessage
		_ = json.Unmarshal(old.Server, &previous)
		resolved, err := ResolveProviderSecrets(raw, previous["acme"])
		if err != nil {
			return nil, err
		}
		server["acme"] = resolved
		profile.Server, _ = json.Marshal(server)
	}
	if len(profile.Provider) == 0 {
		return stored, nil
	}
	var draft ProviderView
	if json.Unmarshal(profile.Provider, &draft) != nil {
		return nil, fmt.Errorf("TLS provider editor data must be an object")
	}
	var reference string
	_ = json.Unmarshal(server["certificate_provider"], &reference)
	if reference != draft.Tag {
		return nil, diagnostics.FirstError([]diagnostics.Finding{providerFinding("tls.server.certificate_provider", "TLS_PROVIDER_REFERENCE_CONFLICT", diagnostics.Error, "The editor definition and reference must have the same stable identity.")})
	}
	old, found, err := sourceProvider(tx, draft.Tag, stored)
	if err != nil {
		return nil, err
	}
	if len(draft.Options) == 0 {
		if !found {
			return nil, fmt.Errorf("TLS provider options are required")
		}
		draft.Options = old.Options
	}
	oldOptions := json.RawMessage(nil)
	if found {
		oldOptions = old.Options
	}
	resolved, err := ResolveProviderSecrets(draft.Options, oldOptions)
	if err != nil {
		return nil, err
	}
	definition := ProviderDefinition{Tag: draft.Tag, Type: draft.Type, RuntimeMode: draft.RuntimeMode, Options: resolved}
	if err := ValidateProviderDefinition(definition, false); err != nil {
		return nil, err
	}
	// Editing a shared definition is explicit. All bound TLS profiles are later
	// cascaded by the TLS save adapter in the same transaction.
	replaced := false
	for i, existing := range stored {
		if existing.Tag == draft.Tag {
			stored[i] = definition
			replaced = true
			break
		}
	}
	if !replaced {
		stored = append(stored, definition)
	}
	return stored, nil
}

func persistProfileProvider(tx *gorm.DB, profile *model.Tls, definitions []ProviderDefinition) error {
	prepared, err := PrepareProviderUpgrade([]model.Tls{*profile}, definitions)
	if err != nil {
		return err
	}
	profile.Server = prepared.Profiles[0].Server
	findings := append(TLSOptionsFindings("server", fmt.Sprintf("tls[%d].server", profile.Id), profile.Server), TLSOptionsFindings("client", fmt.Sprintf("tls[%d].client", profile.Id), profile.Client)...)
	if err := diagnostics.FirstError(findings); err != nil {
		return err
	}
	var server map[string]json.RawMessage
	_ = json.Unmarshal(profile.Server, &server)
	var reference string
	_ = json.Unmarshal(server["certificate_provider"], &reference)
	if reference != "" {
		definition, found := findDefinition(prepared.Providers, reference)
		if !found {
			return fmt.Errorf("TLS provider source is unavailable")
		}
		var enabled bool
		_ = json.Unmarshal(server["enabled"], &enabled)
		if err := ValidateProviderDefinition(definition, enabled); err != nil {
			return err
		}
	}
	return WriteProviderDefinitions(tx, prepared.Providers)
}

func BoundProviderProfileIDs(tx *gorm.DB, tag string) ([]uint, error) {
	rows, err := GetAll(tx)
	if err != nil {
		return nil, err
	}
	var result []uint
	for _, row := range rows {
		var server struct {
			Provider string `json:"certificate_provider"`
		}
		if json.Unmarshal(row.Server, &server) == nil && server.Provider == tag {
			result = append(result, row.Id)
		}
	}
	return result, nil
}
