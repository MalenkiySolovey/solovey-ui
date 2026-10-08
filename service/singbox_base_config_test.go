package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
)

func TestSingBoxBaseProviderDirectSettingsUsesOwnerAndFullPreflight(t *testing.T) {
	if !registry.Resolve("certificateProviders", "acme").Available() {
		t.Skip("this composition has no ACME constructor")
	}
	settings := initSettingTestDB(t)
	if err := dbsqlite.DB().AutoMigrate(&model.TLSCertificateProvider{}); err != nil {
		t.Fatal(err)
	}
	if err := settings.setString("config", `{"log":{"disabled":true},"certificate_providers":[{"type":"acme","tag":"native","domain":["fixture.invalid"],"email":"operator@fixture.invalid"}]}`); err != nil {
		t.Fatal(err)
	}
	stored, err := settings.getString("config")
	if err != nil || strings.Contains(stored, "certificate_providers") {
		t.Fatal("direct settings write retained a second provider store")
	}
	definitions, err := entitytls.ReadProviderDefinitions(dbsqlite.DB())
	if err != nil || len(definitions) != 1 {
		t.Fatal("direct settings write bypassed encrypted TLS storage")
	}
	before := definitions[0]
	if err := settings.setString("config", `{"certificate_providers":[{"type":"acme","tag":"broken"}]}`); err == nil {
		t.Fatal("pinned full constructor failure was accepted")
	}
	after, err := entitytls.ReadProviderDefinitions(dbsqlite.DB())
	if err != nil || len(after) != 1 || string(after[0].Options) != string(before.Options) {
		t.Fatal("failed native preflight changed provider source")
	}
	current, _ := settings.getString("config")
	if current != stored {
		t.Fatal("failed preflight changed base source")
	}
}

func TestSingBoxBaseConfigStoreSetValidatesAndNormalizesConfig(t *testing.T) {
	settingService := initSettingTestDB(t)
	store := NewSingBoxBaseConfigStore(settingService)

	if err := store.Set(`{"dns":{"servers":[]},"route":{"rules":[]}}`); err != nil {
		t.Fatal(err)
	}
	saved, err := store.Get()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved, "\n  \"dns\"") || !strings.Contains(saved, "\n  \"route\"") {
		t.Fatalf("set config was not normalized: %s", saved)
	}

	if err := store.Set(`{"dns":{"servers":{}}}`); err == nil {
		t.Fatal("expected invalid config to be rejected")
	}
}

func TestSingBoxBaseConfigStoreSaveCreatesMissingConfigSetting(t *testing.T) {
	settingService := initSettingTestDB(t)
	tx := dbsqlite.DB().Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}

	config := json.RawMessage(`{"dns":{"servers":[{"type":"local","tag":"dns-umbrella"}]},"route":{"rules":[{"action":"sniff"}]}}`)
	if err := NewSingBoxBaseConfigStore(settingService).Save(tx, config); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}

	var saved string
	if err := dbsqlite.DB().Model(&model.Setting{}).Select("value").Where("key = ?", "config").Scan(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved, `"dns"`) || !strings.Contains(saved, `"route"`) {
		t.Fatalf("saved config does not contain DNS and route data: %s", saved)
	}
}
