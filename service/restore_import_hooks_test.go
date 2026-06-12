package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestRestoreImportServicePostOpenActionsEnsuresDefaultsRotatesAndAudits(t *testing.T) {
	settingService := initSettingTestDB(t)
	db := database.GetDB()
	if err := db.Where("key IN ?", []string{"secret", "sessionGeneration"}).Delete(&model.Setting{}).Error; err != nil {
		t.Fatal(err)
	}

	if err := runRestoreImportServicePostOpenActions(context.Background()); err != nil {
		t.Fatal(err)
	}

	var secretCount int64
	if err := db.Model(model.Setting{}).Where("key = ?", "secret").Count(&secretCount).Error; err != nil {
		t.Fatal(err)
	}
	if secretCount != 1 {
		t.Fatalf("secret default row count=%d, want 1", secretCount)
	}

	generation, err := settingService.GetSessionGeneration()
	if err != nil {
		t.Fatal(err)
	}
	if generation == "" {
		t.Fatal("session generation was not rotated")
	}

	var event model.AuditEvent
	if err := db.Where("event = ?", "db_restore_post_actions").Order("id desc").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Actor != "system" || event.Resource != "database" || event.Severity != AuditSeverityInfo {
		t.Fatalf("unexpected restore audit event: %#v", event)
	}
	details := string(event.Details)
	for _, expected := range []string{
		`"encryptedSettingsResealed":0`,
		`"sessionRotated":true`,
		`"settingsInitialized":true`,
	} {
		if !strings.Contains(details, expected) {
			t.Fatalf("restore audit details missing %s: %s", expected, details)
		}
	}
}

func TestRestoreImportServicePostOpenActionListRunsInOrderAndNamesFailures(t *testing.T) {
	var got []string
	result := restoreImportPostOpenResult{}
	actions := []restoreImportPostOpenAction{
		{
			name: "first",
			run: func(_ context.Context, _ *SettingService, result *restoreImportPostOpenResult) error {
				got = append(got, "first")
				result.settingsInitialized = true
				return nil
			},
		},
		{
			name: "second",
			run: func(_ context.Context, _ *SettingService, result *restoreImportPostOpenResult) error {
				got = append(got, "second")
				result.encryptedSettingsResealed = 2
				return nil
			},
		},
	}
	if err := runRestoreImportServicePostOpenActionList(context.Background(), &SettingService{}, &result, actions); err != nil {
		t.Fatal(err)
	}
	if want := []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("restore action order=%v, want %v", got, want)
	}
	if !result.settingsInitialized || result.encryptedSettingsResealed != 2 {
		t.Fatalf("restore action result not accumulated: %#v", result)
	}

	cause := errors.New("boom")
	err := runRestoreImportServicePostOpenActionList(context.Background(), &SettingService{}, &restoreImportPostOpenResult{}, []restoreImportPostOpenAction{
		{name: "failing action", run: func(context.Context, *SettingService, *restoreImportPostOpenResult) error {
			return cause
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "failing action") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("restore action error was not named: %v", err)
	}
}
