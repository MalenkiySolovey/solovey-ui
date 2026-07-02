package service

import (
	"context"
	"encoding/json"
	"testing"
)

func TestConfigSaveReconcilesAfterComponentEnabledSetting(t *testing.T) {
	initSettingTestDB(t)
	defer RegisterComponentSettingsReconciler(nil)

	var calls int
	RegisterComponentSettingsReconciler(func(context.Context) error {
		calls++
		return nil
	})

	payload, _ := json.Marshal(map[string]string{"telegram.enabled": "false"})
	if _, err := (&ConfigService{}).Save("settings", "set", payload, "", "admin", "example.com"); err != nil {
		t.Fatalf("save component setting: %v", err)
	}
	if calls != 1 {
		t.Fatalf("reconciler calls = %d, want 1", calls)
	}

	payload, _ = json.Marshal(map[string]string{"webDomain": "example.com"})
	if _, err := (&ConfigService{}).Save("settings", "set", payload, "", "admin", "example.com"); err != nil {
		t.Fatalf("save regular setting: %v", err)
	}
	if calls != 1 {
		t.Fatalf("regular setting triggered component reconcile: calls = %d", calls)
	}
}

func TestConfigSaveRejectsInvalidComponentEnabledSetting(t *testing.T) {
	initSettingTestDB(t)
	defer RegisterComponentSettingsReconciler(nil)

	RegisterComponentSettingsReconciler(func(context.Context) error {
		t.Fatal("reconciler must not run after rejected save")
		return nil
	})

	payload, _ := json.Marshal(map[string]string{"telegram.enabled": "definitely"})
	if _, err := (&ConfigService{}).Save("settings", "set", payload, "", "admin", "example.com"); err == nil {
		t.Fatal("invalid component enabled setting was accepted")
	}
}
