package service

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
)

func TestLoadPanelSettingsForDataUsesSingleSnapshotValues(t *testing.T) {
	settingService := initSettingTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	updatePanelLoadSetting(t, settingKeySubURI, "https://override.example/sub/")
	updatePanelLoadSetting(t, settingKeySubJsonURI, "https://json.example/sub")
	updatePanelLoadSetting(t, settingKeySubClashURI, "https://clash.example/sub")
	updatePanelLoadSetting(t, settingKeySubXrayURI, "https://xray.example/sub")
	updatePanelLoadSetting(t, settingcatalog.TrafficAgeKey, "7")

	settings, err := settingService.LoadPanelSettingsForData("panel.example")
	if err != nil {
		t.Fatal(err)
	}
	if settings.SubURI != "https://override.example/sub/" {
		t.Fatalf("SubURI did not use override: %#v", settings)
	}
	if settings.SubJsonURI != "https://json.example/sub" || settings.SubClashURI != "https://clash.example/sub" || settings.SubXrayURI != "https://xray.example/sub" {
		t.Fatalf("format URIs not loaded from snapshot: %#v", settings)
	}
	if settings.TrafficAge != 7 {
		t.Fatalf("TrafficAge=%d, want 7", settings.TrafficAge)
	}
	if settings.Config == "" {
		t.Fatal("Config should be loaded from snapshot")
	}
}

func TestLoadPanelSettingsForDataBuildsSubscriptionURI(t *testing.T) {
	settingService := initSettingTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	updatePanelLoadSetting(t, settingKeySubURI, "")
	updatePanelLoadSetting(t, settingKeySubDomain, "subs.example.com")
	updatePanelLoadSetting(t, settingKeySubCertFile, "cert.pem")
	updatePanelLoadSetting(t, settingKeySubKeyFile, "key.pem")
	updatePanelLoadSetting(t, settingKeySubPort, "443")
	updatePanelLoadSetting(t, settingKeySubPath, "nested")

	settings, err := settingService.LoadPanelSettingsForData("panel.example")
	if err != nil {
		t.Fatal(err)
	}
	if settings.SubURI != "https://subs.example.com/nested/" {
		t.Fatalf("SubURI=%q, want computed https URL", settings.SubURI)
	}
}

func updatePanelLoadSetting(t *testing.T, key string, value string) {
	t.Helper()
	if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", key).Update("value", value).Error; err != nil {
		t.Fatal(err)
	}
}
