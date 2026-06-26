package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
)

func TestConfigSavePlanReturnsCopiedObjects(t *testing.T) {
	plan := newConfigSavePlan(singboxapply.ObjectClients.String())
	plan.IncludeSaveObjects(singboxapply.ObjectInbounds)
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
	tx := dbsqlite.DB().Begin()
	defer tx.Rollback()

	plan := newConfigSavePlan(singboxapply.ObjectSettings.String())
	if err := (&ConfigService{}).applyConfigSaveMutation(tx, &plan, singboxapply.ObjectSettings.String(), "set", payload, "", "example.com"); err != nil {
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
	tx := dbsqlite.DB().Begin()
	defer tx.Rollback()

	plan := newConfigSavePlan(singboxapply.ObjectConfig.String())
	if err := (&ConfigService{}).applyConfigSaveMutation(tx, &plan, singboxapply.ObjectConfig.String(), "set", payload, "", "example.com"); err != nil {
		t.Fatal(err)
	}
	if got := plan.Objects(); !reflect.DeepEqual(got, []string{"config"}) {
		t.Fatalf("unexpected config objects: %#v", got)
	}
	if !plan.RequiresCoreRestart() {
		t.Fatal("config save should require core restart")
	}
}

func TestApplyConfigSaveMutationPlansUnchangedConfigWithoutCoreRestart(t *testing.T) {
	settingService := initSettingTestDB(t)

	payload := json.RawMessage(`{"log":{"level":"info"},"dns":{"servers":[],"rules":[]},"route":{"rules":[]},"experimental":{}}`)
	tx := dbsqlite.DB().Begin()
	if err := settingService.SaveConfig(tx, payload); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}

	tx = dbsqlite.DB().Begin()
	defer tx.Rollback()
	plan := newConfigSavePlan(singboxapply.ObjectConfig.String())
	if err := (&ConfigService{}).applyConfigSaveMutation(tx, &plan, singboxapply.ObjectConfig.String(), "set", payload, "", "example.com"); err != nil {
		t.Fatal(err)
	}
	if plan.RequiresCoreRestart() {
		t.Fatal("unchanged config save should not require core restart")
	}
}

func TestApplyConfigSaveMutationRejectsUnsafeLogOutput(t *testing.T) {
	initSettingTestDB(t)

	payload := json.RawMessage(`{"log":{"level":"info","output":"../panel.log"},"dns":{"servers":[],"rules":[]},"route":{"rules":[]},"experimental":{}}`)
	tx := dbsqlite.DB().Begin()
	defer tx.Rollback()

	plan := newConfigSavePlan(singboxapply.ObjectConfig.String())
	err := (&ConfigService{}).applyConfigSaveMutation(tx, &plan, singboxapply.ObjectConfig.String(), "set", payload, "", "example.com")
	if err == nil || !strings.Contains(err.Error(), "log.output") {
		t.Fatalf("expected unsafe log.output error, got %v", err)
	}
}
