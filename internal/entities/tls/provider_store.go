package entitytls

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	settingscrypto "github.com/MalenkiySolovey/solovey-ui/internal/settings/crypto"
	settingsstore "github.com/MalenkiySolovey/solovey-ui/internal/settings/store"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/MalenkiySolovey/solovey-ui/util/redact"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"github.com/sagernet/sing-box/option"
	sbjson "github.com/sagernet/sing/common/json"
	"gorm.io/gorm"
)

var providerTagPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)

const MaxProviders = 1024
const MaxProviderOptionsBytes = 1 << 20

// ProviderCodec reads the same settings master from the supplied candidate
// database. It never falls through to the published process-wide DB or creates
// a master while rendering a preview.
func ProviderCodec(db *gorm.DB) settingscrypto.Codec {
	return settingscrypto.Codec{MasterSecret: func() ([]byte, error) {
		if db == nil {
			return nil, errors.New("TLS master-key view is unavailable")
		}
		row, err := settingsstore.Find(db, settingcatalog.SecretKey)
		if err != nil || row.Value == "" {
			return nil, errors.New("TLS master-key view is unavailable")
		}
		return []byte(row.Value), nil
	}}
}

func providerAAD(tag string) string { return "tls-provider:v1:" + tag }

func ReadProviderDefinitions(db *gorm.DB) ([]ProviderDefinition, error) {
	result := []ProviderDefinition{}
	if db == nil {
		return nil, errors.New("TLS provider database is unavailable")
	}
	if !db.Migrator().HasTable(&model.TLSCertificateProvider{}) {
		return result, nil
	}
	var records []model.TLSCertificateProvider
	if err := db.Order("id").Limit(MaxProviders + 1).Find(&records).Error; err != nil {
		return nil, err
	}
	if len(records) > MaxProviders {
		return nil, errors.New("TLS provider inventory exceeds its bound")
	}
	codec := ProviderCodec(db)
	for _, record := range records {
		if !secretbox.IsEncrypted(record.OptionsEnvelope) {
			return nil, diagnostics.FirstError([]diagnostics.Finding{providerFinding("certificate_providers", "TLS_PROVIDER_ENVELOPE_INVALID", diagnostics.Error, "Provider options require an authenticated envelope; plaintext fallback is forbidden.")})
		}
		plaintext, err := codec.DecryptString(providerAAD(record.Tag), record.OptionsEnvelope)
		if err != nil {
			return nil, diagnostics.FirstError([]diagnostics.Finding{providerFinding("certificate_providers", "TLS_PROVIDER_DECRYPT_FAILED", diagnostics.Error, "The provider cannot be opened with this database's approved key candidates. Restore its key lifecycle before retrying.")})
		}
		definition := ProviderDefinition{Tag: record.Tag, Type: record.Type, RuntimeMode: record.RuntimeMode, Options: json.RawMessage(plaintext)}
		if err := ValidateProviderDefinition(definition, false); err != nil {
			return nil, err
		}
		result = append(result, definition)
	}
	return result, nil
}

func ValidateProviderDefinition(provider ProviderDefinition, activation bool) error {
	reject := func(code, message string) error {
		return diagnostics.FirstError([]diagnostics.Finding{providerFinding("certificate_providers", code, diagnostics.Error, message)})
	}
	if !providerTagPattern.MatchString(provider.Tag) {
		return reject("TLS_PROVIDER_IDENTITY_INVALID", "Provider identity must be a bounded stable tag.")
	}
	if provider.Type != "acme" {
		return reject("TLS_PROVIDER_UNAVAILABLE", "This provider lacks the complete build, HTTP, secret, file and platform authorization contract.")
	}
	if provider.RuntimeMode != LegacyInlineMode && provider.RuntimeMode != NativeProviderMode {
		return reject("TLS_PROVIDER_MODE_INVALID", "Select the accepted legacy adapter or the current native provider mode.")
	}
	if activation && !registry.Resolve("certificateProviders", provider.Type).Available() {
		return reject("TLS_PROVIDER_UNAVAILABLE", "The provider is not available in this build and product composition.")
	}
	var options map[string]json.RawMessage
	if len(provider.Options) > MaxProviderOptionsBytes || json.Unmarshal(provider.Options, &options) != nil || options == nil {
		return reject("TLS_PROVIDER_OPTIONS_INVALID", "Provider options must be an object accepted by the pinned schema.")
	}
	var authority string
	_ = json.Unmarshal(options["provider"], &authority)
	if _, err := ACMEAuthority(authority); err != nil {
		return reject("TLS_ACME_AUTHORITY_INVALID", "Select an accepted ACME authority.")
	}
	if provider.RuntimeMode == LegacyInlineMode {
		// The pinned decoder owns the accepted legacy grammar. This adapter only
		// chooses the consumer; it does not translate into native HTTP semantics.
		raw, _ := json.Marshal(map[string]any{"enabled": true, "acme": json.RawMessage(provider.Options)})
		var tls option.InboundTLSOptions
		if sbjson.UnmarshalContext(registry.Context(context.Background()), raw, &tls) != nil {
			return reject("TLS_PROVIDER_OPTIONS_INVALID", "Legacy ACME options are rejected by the pinned schema.")
		}
	} else {
		options["type"], _ = json.Marshal(provider.Type)
		options["tag"], _ = json.Marshal(provider.Tag)
		raw, _ := json.Marshal(options)
		var pinned option.CertificateProvider
		if pinned.UnmarshalJSONContext(registry.Context(context.Background()), raw) != nil {
			return reject("TLS_PROVIDER_OPTIONS_INVALID", "Native provider options are rejected by the pinned schema.")
		}
	}
	return nil
}

// WriteProviderDefinitions is called only on the caller's candidate transaction
// after semantic validation. Unchanged definitions retain exact envelope bytes,
// so repeating a migration does not reseal or create a third durable state.
func WriteProviderDefinitions(tx *gorm.DB, definitions []ProviderDefinition) error {
	if tx == nil {
		return errors.New("TLS provider database is unavailable")
	}
	return tx.Transaction(func(candidate *gorm.DB) error { return writeProviderDefinitions(candidate, definitions) })
}

func writeProviderDefinitions(tx *gorm.DB, definitions []ProviderDefinition) error {
	if len(definitions) > MaxProviders {
		return errors.New("TLS provider inventory exceeds its bound")
	}
	existing, err := ReadProviderDefinitions(tx)
	if err != nil {
		return err
	}
	byTag := map[string]ProviderDefinition{}
	for _, value := range existing {
		byTag[value.Tag] = value
	}
	seen := map[string]bool{}
	for _, definition := range definitions {
		if seen[definition.Tag] {
			return errors.New("TLS_PROVIDER_TAG_CONFLICT: provider identities must be unique")
		}
		seen[definition.Tag] = true
		if err := ValidateProviderDefinition(definition, false); err != nil {
			return err
		}
	}
	for _, definition := range definitions {
		if err := ValidateProviderDefinition(definition, false); err != nil {
			return err
		}
		if old, found := byTag[definition.Tag]; found && old.Type == definition.Type && old.RuntimeMode == definition.RuntimeMode && providerJSONEqual(old.Options, definition.Options) {
			continue
		}
		// Portable provider envelopes use the existing database master candidate.
		// Read accepts approved env/legacy candidates; backup never depends on a
		// source host's optional environment key merely to open these records.
		if _, err := settingsstore.EnsureMasterSecret(tx); err != nil {
			return errors.New("TLS provider master lifecycle is unavailable")
		}
		candidates, err := ProviderCodec(tx).SettingsSecretboxCandidates()
		var envelope string
		if err == nil && len(candidates) > 0 {
			envelope, err = candidates[0].Box.EncryptString(string(definition.Options), providerAAD(definition.Tag))
		}
		if err != nil {
			return diagnostics.FirstError([]diagnostics.Finding{providerFinding("certificate_providers", "TLS_PROVIDER_ENCRYPT_FAILED", diagnostics.Error, "Provider options cannot be sealed with this database's master lifecycle.")})
		}
		if !secretbox.IsEncrypted(envelope) {
			return errors.New("TLS provider encryption did not produce an envelope")
		}
		var row model.TLSCertificateProvider
		result := tx.Where("tag = ?", definition.Tag).Limit(1).Find(&row)
		if result.Error != nil {
			return result.Error
		}
		row.Tag, row.Type, row.RuntimeMode, row.OptionsEnvelope = definition.Tag, definition.Type, definition.RuntimeMode, envelope
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		byTag[definition.Tag] = definition
	}
	return nil
}

type ProviderView struct {
	Tag         string          `json:"tag"`
	Type        string          `json:"type"`
	RuntimeMode string          `json:"runtimeMode"`
	Options     json.RawMessage `json:"options"`
}

func providerSecretKey(key string) bool {
	return redact.IsSensitiveKey(key) || key == "account_key" || key == "mac_key"
}

func MaskProviderOptions(source json.RawMessage) json.RawMessage {
	var value any
	if json.Unmarshal(source, &value) != nil {
		return json.RawMessage(`{}`)
	}
	var mask func(any) any
	mask = func(value any) any {
		switch item := value.(type) {
		case map[string]any:
			for key, v := range item {
				if providerSecretKey(key) {
					if text, ok := v.(string); ok && text == "" {
						continue
					}
					item[key] = redact.Marker
				} else {
					item[key] = mask(v)
				}
			}
		case []any:
			for i, v := range item {
				item[i] = mask(v)
			}
		}
		return value
	}
	raw, _ := json.Marshal(mask(value))
	return raw
}

// ResolveProviderSecrets treats absent secret fields/markers as unchanged,
// explicit empty values as clear, and new values as replacement. A marker has
// no authority to invent a secret in a new or differently keyed definition.
func ResolveProviderSecrets(draft, stored json.RawMessage) (json.RawMessage, error) {
	var incoming, old map[string]any
	if json.Unmarshal(draft, &incoming) != nil || incoming == nil {
		return nil, errors.New("TLS provider draft must be an object")
	}
	if len(stored) > 0 && json.Unmarshal(stored, &old) != nil {
		return nil, errors.New("TLS provider stored options are invalid")
	}
	var merge func(map[string]any, map[string]any) error
	merge = func(now, before map[string]any) error {
		for key, value := range now {
			if providerSecretKey(key) && value == redact.Marker {
				previous, ok := before[key]
				if !ok {
					return diagnostics.FirstError([]diagnostics.Finding{providerFinding("certificate_providers.options", "TLS_SECRET_UNAVAILABLE", diagnostics.Error, "A stored-secret marker has no matching source. Replace it explicitly before retrying.")})
				}
				now[key] = previous
				continue
			}
			if child, ok := value.(map[string]any); ok {
				previous, _ := before[key].(map[string]any)
				if err := merge(child, previous); err != nil {
					return err
				}
			}
		}
		for key, value := range before {
			if _, present := now[key]; !present && providerSecretKey(key) {
				now[key] = value
			}
		}
		return nil
	}
	if err := merge(incoming, old); err != nil {
		return nil, err
	}
	return json.Marshal(incoming)
}

func ProviderViews(db *gorm.DB) ([]ProviderView, error) {
	definitions, err := ReadProviderDefinitions(db)
	if err != nil {
		return nil, err
	}
	result := make([]ProviderView, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, ProviderView{Tag: definition.Tag, Type: definition.Type, RuntimeMode: definition.RuntimeMode, Options: MaskProviderOptions(definition.Options)})
	}
	return result, nil
}
