package service

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database"
)

func TestConfigSavePlanReturnsCopiedObjects(t *testing.T) {
	plan := newConfigSavePlan(configSaveObjectClients.String())
	plan.IncludeSaveObjects(configSaveObjectInbounds)
	plan.RequireCoreRestart()

	objects := plan.Objects()
	objects[0] = "mutated"

	if got := plan.Objects(); !reflect.DeepEqual(got, []string{"clients", "inbounds"}) {
		t.Fatalf("plan objects were mutated through returned slice: %#v", got)
	}
	if !plan.RequiresCoreRestart() {
		t.Fatal("expected core restart flag")
	}
}

func TestApplyConfigSaveMutationPlansSettingsWithoutCoreRestart(t *testing.T) {
	t.Setenv("SUI_SECRETBOX_KEY", encodedTestSecretboxKey())
	initSettingTestDB(t)

	payload, err := json.Marshal(map[string]string{
		"telegramChatID": "42",
	})
	if err != nil {
		t.Fatal(err)
	}
	tx := database.GetDB().Begin()
	defer tx.Rollback()

	plan := newConfigSavePlan(configSaveObjectSettings.String())
	if err := (&ConfigService{}).applyConfigSaveMutation(tx, &plan, configSaveObjectSettings.String(), "set", payload, "", "example.com"); err != nil {
		t.Fatal(err)
	}
	if got := plan.Objects(); !reflect.DeepEqual(got, []string{"settings"}) {
		t.Fatalf("unexpected settings objects: %#v", got)
	}
	if plan.RequiresCoreRestart() {
		t.Fatal("settings save should not require core restart")
	}
}

func TestApplyConfigSaveMutationPlansConfigCoreRestart(t *testing.T) {
	initSettingTestDB(t)

	payload := json.RawMessage(`{"log":{"level":"info"},"dns":{"servers":[],"rules":[]},"route":{"rules":[]},"experimental":{}}`)
	tx := database.GetDB().Begin()
	defer tx.Rollback()

	plan := newConfigSavePlan(configSaveObjectConfig.String())
	if err := (&ConfigService{}).applyConfigSaveMutation(tx, &plan, configSaveObjectConfig.String(), "set", payload, "", "example.com"); err != nil {
		t.Fatal(err)
	}
	if got := plan.Objects(); !reflect.DeepEqual(got, []string{"config"}) {
		t.Fatalf("unexpected config objects: %#v", got)
	}
	if !plan.RequiresCoreRestart() {
		t.Fatal("config save should require core restart")
	}
}
