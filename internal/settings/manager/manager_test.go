package manager

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	settingsschema "github.com/MalenkiySolovey/solovey-ui/internal/settings/schema"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type testSecretCodec struct{}

func (testSecretCodec) EncryptString(key string, value string) (string, error) {
	return "enc:" + key + ":" + value, nil
}

func (testSecretCodec) DecryptString(key string, value string) (string, error) {
	return strings.TrimPrefix(value, "enc:"+key+":"), nil
}

func (testSecretCodec) WriteMarker(settings map[string]string, key string, value string) {
	settings[key+"HasSecret"] = "true"
}

func newManagerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "CGO_ENABLED=0") {
			t.Skip("sqlite driver requires CGO in this environment")
		}
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatalf("migrate settings: %v", err)
	}
	return db
}

func newTestManager(db *gorm.DB) Manager {
	schema := settingsschema.New(
		map[string]string{
			"visible": "default-visible",
			"secret":  "",
			"hidden":  "internal",
		},
		map[string]struct{}{"hidden": {}},
		map[string]struct{}{"secret": {}},
	)
	return Manager{
		DB:     func() *gorm.DB { return db },
		Schema: schema,
		Secret: testSecretCodec{},
		Hooks: Hooks{
			CanClearEmptyEncrypted: func(key string) bool { return key == "secret" },
		},
	}
}

func TestManagerSaveEncryptsAndGetAllMasksSecret(t *testing.T) {
	db := newManagerTestDB(t)
	manager := newTestManager(db)

	payload, _ := json.Marshal(map[string]string{
		"visible": "changed",
		"secret":  "value",
	})
	if err := manager.Save(db, payload); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := manager.GetString("secret")
	if err != nil {
		t.Fatalf("get secret: %v", err)
	}
	if got != "value" {
		t.Fatalf("secret = %q, want value", got)
	}

	settings, err := manager.GetAll()
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if settings["visible"] != "changed" {
		t.Fatalf("visible = %q, want changed", settings["visible"])
	}
	if _, ok := settings["secret"]; ok {
		t.Fatalf("secret leaked through GetAll: %#v", settings)
	}
	if settings["secretHasSecret"] != "true" {
		t.Fatalf("missing secret marker: %#v", settings)
	}
	if _, ok := settings["hidden"]; ok {
		t.Fatalf("internal setting leaked through GetAll: %#v", settings)
	}
}

func TestManagerSaveRejectsUnknownAndInternalKeys(t *testing.T) {
	db := newManagerTestDB(t)
	manager := newTestManager(db)

	for _, key := range []string{"unknown", "hidden"} {
		payload, _ := json.Marshal(map[string]string{key: "value"})
		if err := manager.Save(db, payload); err == nil {
			t.Fatalf("expected %s to be rejected", key)
		}
	}
}

func TestManagerGetAllSkipsRowsOutsideActiveSchema(t *testing.T) {
	db := newManagerTestDB(t)
	manager := newTestManager(db)

	if err := db.Create(&model.Setting{Key: "visible", Value: "stored-visible"}).Error; err != nil {
		t.Fatalf("seed visible: %v", err)
	}
	if err := db.Create(&model.Setting{Key: "component.removed.setting", Value: "stale"}).Error; err != nil {
		t.Fatalf("seed removed component setting: %v", err)
	}

	settings, err := manager.GetAll()
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if settings["visible"] != "stored-visible" {
		t.Fatalf("visible = %q, want stored-visible", settings["visible"])
	}
	if _, ok := settings["component.removed.setting"]; ok {
		t.Fatalf("inactive component setting leaked through GetAll: %#v", settings)
	}
}

func TestManagerSaveAllowsHookApprovedDynamicKeys(t *testing.T) {
	db := newManagerTestDB(t)
	manager := newTestManager(db)
	manager.Hooks.CanSaveKey = func(key string) bool {
		return key == "component.enabled"
	}

	payload, _ := json.Marshal(map[string]string{"component.enabled": "false"})
	if err := manager.Save(db, payload); err != nil {
		t.Fatalf("save dynamic key: %v", err)
	}

	got, err := manager.GetString("component.enabled")
	if err != nil {
		t.Fatalf("get dynamic key: %v", err)
	}
	if got != "false" {
		t.Fatalf("dynamic key = %q, want false", got)
	}
}

func TestManagerSaveSkipsStoredSecretMarker(t *testing.T) {
	db := newManagerTestDB(t)
	manager := newTestManager(db)
	manager.StoredSecret = "stored"

	first, _ := json.Marshal(map[string]string{"secret": "value"})
	if err := manager.Save(db, first); err != nil {
		t.Fatalf("save first secret: %v", err)
	}
	second, _ := json.Marshal(map[string]string{"secret": "stored"})
	if err := manager.Save(db, second); err != nil {
		t.Fatalf("save marker: %v", err)
	}

	var setting model.Setting
	if err := db.Where("key = ?", "secret").First(&setting).Error; err != nil {
		t.Fatalf("read secret row: %v", err)
	}
	if setting.Value != "enc:secret:value" {
		t.Fatalf("stored marker overwrote secret: %q", setting.Value)
	}
}
