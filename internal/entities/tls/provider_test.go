package entitytls

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/redact"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/gorm"
)

func TestTLSProviderMissingMasterWriteIsAtomic(t *testing.T) {
	db := providerDB(t)
	if err := db.Where("key = ?", "secret").Delete(&model.Setting{}).Error; err != nil {
		t.Fatal(err)
	}
	definition := ProviderDefinition{Tag: "atomic", Type: "acme", RuntimeMode: LegacyInlineMode, Options: json.RawMessage(`{"domain":["fixture.invalid"]}`)}
	if _, err := ReadProviderDefinitions(db); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&model.Setting{}).Where("key = ?", "secret").Count(&count)
	if count != 0 {
		t.Fatal("preview created a master")
	}
	bad := definition
	bad.Tag = "invalid tag"
	if err := WriteProviderDefinitions(db, []ProviderDefinition{definition, bad}); err == nil {
		t.Fatal("invalid later provider accepted")
	}
	db.Model(&model.Setting{}).Where("key = ?", "secret").Count(&count)
	if count != 0 {
		t.Fatal("rejected inventory published a master")
	}
	abort := errors.New("later candidate failure")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := WriteProviderDefinitions(tx, []ProviderDefinition{definition}); err != nil {
			return err
		}
		return abort
	}); !errors.Is(err, abort) {
		t.Fatal("candidate did not abort")
	}
	db.Model(&model.Setting{}).Where("key = ?", "secret").Count(&count)
	if count != 0 {
		t.Fatal("rollback retained candidate master")
	}
	db.Model(&model.TLSCertificateProvider{}).Count(&count)
	if count != 0 {
		t.Fatal("rollback retained provider")
	}
	if err := WriteProviderDefinitions(db, []ProviderDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("key = ?", "secret").Delete(&model.Setting{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := WriteProviderDefinitions(db, []ProviderDefinition{definition}); err == nil {
		t.Fatal("missing source master was regenerated for unreadable records")
	}
	db.Model(&model.Setting{}).Where("key = ?", "secret").Count(&count)
	if count != 0 {
		t.Fatal("unreadable source rotated its master")
	}
}

func providerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newTLSDB(t)
	if err := db.AutoMigrate(&model.Setting{}, &model.TLSCertificateProvider{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Setting{Key: "secret", Value: strings.Repeat("a", 32)}).Error; err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAcceptedOldTLSProviderReplay(t *testing.T) {
	var manifest []struct {
		ID     string `json:"fixture_id"`
		Hash   string `json:"config_sha256"`
		Commit string `json:"solovey_commit"`
		Core   string `json:"core_version"`
	}
	raw, err := os.ReadFile("testdata/accepted-old/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range manifest {
		t.Run(fixture.ID, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join("testdata/accepted-old", fixture.ID+".json"))
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(source)
			if hex.EncodeToString(hash[:]) != fixture.Hash || fixture.Commit != "3523eeea9f91c091ab3f0fbdb94c3955e37798b4" || fixture.Core != "v1.13.18" {
				t.Fatal("old fixture identity differs from the accepted capture")
			}
			var profile model.Tls
			if err := json.Unmarshal(source, &profile); err != nil {
				t.Fatal(err)
			}
			prepared, err := PrepareProviderUpgrade([]model.Tls{profile}, nil)
			if err != nil {
				t.Fatal(err)
			}
			repeated, err := PrepareProviderUpgrade(prepared.Profiles, prepared.Providers)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(repeated.Profiles, prepared.Profiles) || !reflect.DeepEqual(repeated.Providers, prepared.Providers) {
				t.Fatal("second transform changed desired state")
			}
			var before, after map[string]json.RawMessage
			_ = json.Unmarshal(profile.Server, &before)
			_ = json.Unmarshal(prepared.Profiles[0].Server, &after)
			if original, has := before["acme"]; has {
				if len(prepared.Providers) != 1 || prepared.Providers[0].RuntimeMode != LegacyInlineMode || !providerJSONEqual(original, prepared.Providers[0].Options) {
					t.Fatal("ACME semantic preimage was changed")
				}
				if _, present := after["acme"]; present {
					t.Fatal("duplicate persistent inline options remain")
				}
				if registry.Resolve("certificateProviders", "acme").Available() {
					config, _ := json.Marshal(map[string]any{"inbounds": []any{map[string]any{"type": "trojan", "tag": "fixture", "tls": prepared.Profiles[0].Server}}})
					projected, err := ProjectCertificateProviders(config, prepared.Providers)
					if err != nil {
						t.Fatal(err)
					}
					var runtime struct {
						Inbounds []struct {
							TLS map[string]json.RawMessage `json:"tls"`
						} `json:"inbounds"`
						Providers json.RawMessage `json:"certificate_providers"`
					}
					_ = json.Unmarshal(projected.Candidate, &runtime)
					if len(runtime.Providers) > 0 || !providerJSONEqual(runtime.Inbounds[0].TLS["acme"], original) {
						t.Fatal("legacy runtime adapter changed transport/account semantics")
					}
				}
			}
		})
	}
}

func TestTLSProviderCollisionAndConflictPreimage(t *testing.T) {
	profile := model.Tls{Id: 7, Server: json.RawMessage(`{"enabled":false,"acme":{"domain":["fixture.invalid"],"disable_http_challenge":false}}`), Client: json.RawMessage(`{"insecure":false}`)}
	existing := []ProviderDefinition{{Tag: "tls-acme-7", Type: "acme", RuntimeMode: LegacyInlineMode, Options: json.RawMessage(`{"domain":["other.invalid"]}`)}}
	prepared, err := PrepareProviderUpgrade([]model.Tls{profile}, existing)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Providers) != 2 || prepared.Providers[1].Tag != "tls-acme-7-2" {
		t.Fatal("collision did not produce deterministic stable identity")
	}
	conflict := profile
	conflict.Server = json.RawMessage(`{"acme":{"domain":["fixture.invalid"]},"certificate_provider":"tls-acme-7"}`)
	failed, err := PrepareProviderUpgrade([]model.Tls{profile, conflict}, existing)
	if err == nil || !reflect.DeepEqual(failed.Profiles, []model.Tls{profile, conflict}) || !reflect.DeepEqual(failed.Providers, existing) {
		t.Fatal("conflict did not retain the whole source preimage")
	}
	if !strings.Contains(err.Error(), "TLS_PROVIDER_CONFLICT") {
		t.Fatal("conflict lacks stable reason")
	}
}

func TestTLSProviderEnvelopePortableIdempotentAndKeyBound(t *testing.T) {
	db := providerDB(t)
	t.Setenv("SUI_SECRETBOX_KEY", strings.Repeat("1", 64))
	definition := ProviderDefinition{Tag: "fixture-provider", Type: "acme", RuntimeMode: NativeProviderMode, Options: json.RawMessage(`{"domain":["fixture.invalid"],"account_key":"inert-fixture-value"}`)}
	if err := WriteProviderDefinitions(db, []ProviderDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	var first model.TLSCertificateProvider
	if err := db.First(&first).Error; err != nil {
		t.Fatal(err)
	}
	if !secretbox.IsEncrypted(first.OptionsEnvelope) || strings.Contains(first.OptionsEnvelope, "inert-fixture-value") {
		t.Fatal("provider is not sealed")
	}
	if err := WriteProviderDefinitions(db, []ProviderDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	var second model.TLSCertificateProvider
	_ = db.First(&second).Error
	if first.OptionsEnvelope != second.OptionsEnvelope {
		t.Fatal("repeat save resealed unchanged bytes")
	}
	t.Setenv("SUI_SECRETBOX_KEY", "")
	portable := providerDB(t)
	if err := portable.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	read, err := ReadProviderDefinitions(portable)
	if err != nil || len(read) != 1 || !providerJSONEqual(read[0].Options, definition.Options) {
		t.Fatal("database-owned envelope depends on source host environment")
	}
	if err := portable.Model(&model.Setting{}).Where("key = ?", "secret").Update("value", strings.Repeat("b", 32)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ReadProviderDefinitions(portable); err == nil || !strings.Contains(err.Error(), "TLS_PROVIDER_DECRYPT_FAILED") {
		t.Fatal("wrong candidate database key did not fail closed")
	}
	if err := db.Model(&model.TLSCertificateProvider{}).Where("id = ?", first.Id).Update("options_envelope", string(definition.Options)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ReadProviderDefinitions(db); err == nil || !strings.Contains(err.Error(), "TLS_PROVIDER_ENVELOPE_INVALID") {
		t.Fatal("plaintext provider fallback was admitted")
	}
}

func TestTLSProviderMaskedEditAndSaveFailurePreimage(t *testing.T) {
	db := providerDB(t)
	profile := model.Tls{Name: "fixture", Server: json.RawMessage(`{"enabled":false,"acme":{"domain":["fixture.invalid"],"external_account":{"key_id":"fixture-id","mac_key":"inert-fixture-value"}}}`), Client: json.RawMessage(`{"insecure":false}`)}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	views, err := GetAllViews(db)
	if err != nil || len(views) != 1 {
		t.Fatal("TLS view unavailable")
	}
	if bytes.Contains(views[0].Provider, []byte("inert-fixture-value")) || !bytes.Contains(views[0].Provider, []byte(redact.Marker)) {
		t.Fatal("normal editor exposes decrypted provider secret")
	}
	data, _ := json.Marshal(views[0])
	if err := Save(SaveRequest{Tx: db, Action: "edit", Data: data}); err != nil {
		t.Fatal(err)
	}
	definitions, err := ReadProviderDefinitions(db)
	if err != nil || len(definitions) != 1 || !bytes.Contains(definitions[0].Options, []byte("inert-fixture-value")) {
		t.Fatal("masked edit did not preserve source-owned secret")
	}
	var saved model.Tls
	_ = db.First(&saved, profile.Id).Error
	var envelope model.TLSCertificateProvider
	_ = db.First(&envelope).Error
	failed := views[0]
	failed.Client = json.RawMessage(`{"enabled":true,"client_certificate":["inert-certificate"]}`)
	data, _ = json.Marshal(failed)
	if err := Save(SaveRequest{Tx: db, Action: "edit", Data: data}); err == nil {
		t.Fatal("incomplete mTLS pair accepted")
	}
	var after model.Tls
	_ = db.First(&after, profile.Id).Error
	var afterEnvelope model.TLSCertificateProvider
	_ = db.First(&afterEnvelope).Error
	if !reflect.DeepEqual(after, saved) || !reflect.DeepEqual(afterEnvelope, envelope) {
		t.Fatal("failed save changed durable preimage")
	}
}

func TestTLSProviderSecretIntentAndUnavailableCapabilities(t *testing.T) {
	stored := json.RawMessage(`{"external_account":{"key_id":"fixture-id","mac_key":"inert-fixture-value"}}`)
	for _, draft := range []json.RawMessage{json.RawMessage(`{"external_account":{"key_id":"fixture-id"}}`), MaskProviderOptions(stored)} {
		resolved, err := ResolveProviderSecrets(draft, stored)
		if err != nil || !providerJSONEqual(resolved, stored) {
			t.Fatal("absent/marker did not preserve secret")
		}
	}
	resolved, err := ResolveProviderSecrets(json.RawMessage(`{"external_account":{"mac_key":""}}`), stored)
	if err != nil || bytes.Contains(resolved, []byte("inert-fixture-value")) {
		t.Fatal("explicit clear was ignored")
	}
	if _, err := ResolveProviderSecrets(MaskProviderOptions(stored), nil); err == nil {
		t.Fatal("new marker invented a source secret")
	}
	for _, kind := range []string{"origin_ca", "tailscale"} {
		if err := ValidateProviderDefinition(ProviderDefinition{Tag: "fixture", Type: kind, RuntimeMode: NativeProviderMode, Options: json.RawMessage(`{}`)}, false); err == nil || !strings.Contains(err.Error(), "TLS_PROVIDER_UNAVAILABLE") {
			t.Fatal("registration authorized an unavailable product provider")
		}
	}
}
