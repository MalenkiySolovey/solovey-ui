package service

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	opsdoctor "github.com/MalenkiySolovey/solovey-ui/internal/ops/doctor"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
)

func TestRuleValidationRejectsSaveBeforeCommitAndLiveEffects(t *testing.T) {
	settings := initSettingTestDB(t)
	store := NewSingBoxBaseConfigStore(settings)
	if err := store.Set(`{"log":{"disabled":true},"route":{"rules":[]}}`); err != nil {
		t.Fatal(err)
	}
	before, err := store.Get()
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := &recordingConfigCoreLifecycle{}
	runtime := NewRuntime(runningCoreForConfigSaveTest(t))
	svc := &ConfigService{Runtime: runtime, coreLifecycle: lifecycle}
	svc.setLastUpdate(12345)
	var changesBefore int64
	if err := dbsqlite.DB().Model(&model.Changes{}).Count(&changesBefore).Error; err != nil {
		t.Fatal(err)
	}
	for _, payload := range []json.RawMessage{
		json.RawMessage(`{"route":{"rules":[{"type":"logical","mode":"and","rules":[]}]}}`),
		json.RawMessage(`{"dns":{"rules":[{"type":"logical","mode":"or","rules":[{"type":"logical","mode":"and","rules":[]}]}]}}`),
		json.RawMessage(`{"route":{"rules":[{"type":"logical","mode":"secret-fixture-marker","rules":[{"action":"reject"}]}]}}`),
		json.RawMessage(`{"route":{"rules":[{"password-fixture-marker":"secret-fixture-marker"}]}}`),
		json.RawMessage(`{"route":{"rules":[{"type":"logical","mode":"or","action":"reject","rules":[{"domain":"fixture.example","action":"reject"}]}]}}`),
	} {
		_, err := svc.Save("config", "set", payload, "", "admin", "fixture.example")
		var typed *singboxvalidation.RuleConditionError
		if !errors.As(err, &typed) {
			t.Fatalf("save did not fail at the condition owner: %v", err)
		}
		if strings.Contains(err.Error(), "secret-fixture-marker") || strings.Contains(err.Error(), "password-fixture-marker") {
			t.Fatal("save error leaked submitted values")
		}
		for _, finding := range singboxvalidation.AnalyzeRuleConditions(payload) {
			if finding.Severity == singboxvalidation.RuleSeverityError {
				if !reflect.DeepEqual(typed.Finding, finding) {
					t.Fatalf("save changed the owner finding: %#v != %#v", typed.Finding, finding)
				}
				break
			}
		}
		after, err := store.Get()
		if err != nil || after != before {
			t.Fatalf("invalid save changed storage: %v", err)
		}
		var changesAfter int64
		if err := dbsqlite.DB().Model(&model.Changes{}).Count(&changesAfter).Error; err != nil {
			t.Fatal(err)
		}
		if changesAfter != changesBefore || svc.getLastUpdate() != 12345 || len(lifecycle.calls) != 0 {
			t.Fatal("invalid save committed change rows, change marker or core effects")
		}
	}
	// Warning-only decoder loss keeps the accepted upstream admission behavior.
	if _, err := svc.Save("config", "set", json.RawMessage(`{"log":{"disabled":true},"dns":{"rules":[{}]},"route":{"rules":[{"action":"reject"}]}}`), "", "admin", "fixture.example"); err != nil {
		t.Fatalf("warning-only save rejected: %v", err)
	}
	if len(lifecycle.calls) != 1 || lifecycle.calls[0] != "restart" || svc.getLastUpdate() == 12345 {
		t.Fatal("successful warning-only save did not publish normal effects")
	}
}

func TestRuleValidationCoversEveryBaseConfigMutation(t *testing.T) {
	store := NewSingBoxBaseConfigStore(initSettingTestDB(t))
	payload := json.RawMessage(`{"route":{"rules":[{"type":"logical","mode":"and","rules":[]}]}}`)
	var typed *singboxvalidation.RuleConditionError
	if err := store.Set(string(payload)); !errors.As(err, &typed) {
		t.Fatalf("Set bypassed validation: %v", err)
	}
	tx := dbsqlite.DB().Begin()
	defer tx.Rollback()
	if err := store.Save(tx, payload); !errors.As(err, &typed) {
		t.Fatalf("Save bypassed validation: %v", err)
	}
	if _, err := store.Changed(tx, payload); !errors.As(err, &typed) {
		t.Fatalf("Changed bypassed validation: %v", err)
	}
}

func TestDoctorProjectsHistoricalRuleFindingsWithoutChangingState(t *testing.T) {
	initDoctorTestDB(t)
	replaceDefaultRuntimeForTest(t, NewRuntimeWithCoreProvider(nil))
	for _, payload := range []string{
		`{"route":`,
		`{"log":{"disabled":true},"dns":{"rules":[{"type":"logical","mode":"or","rules":[{"type":"logical","mode":"and","rules":[]}]}]},"route":{"rules":[]}}`,
		`{"log":{"disabled":true},"dns":{"rules":[{}]},"route":{"rules":[]}}`,
		`{"log":{"disabled":true},"dns":{"rules":[]},"route":{"rules":[{"type":"logical","mode":"and","rules":[{"password-fixture-marker":"secret-fixture-marker"}]}]}}`,
	} {
		// Simulate an existing database created before server-side acceptance was
		// repaired. Diagnostics must remain able to read it without normalizing it.
		if err := dbsqlite.DB().Where("key = ?", "config").Assign(model.Setting{Value: payload}).FirstOrCreate(&model.Setting{Key: "config"}).Error; err != nil {
			t.Fatal(err)
		}
		report := (&DoctorService{}).Run("fixture.example")
		compatibility, _ := singboxvalidation.PrepareRuleUpgrade([]byte(payload))
		for _, expected := range compatibility.Findings {
			found := false
			for _, item := range report.Items {
				finding, ok := item.Details.(singboxvalidation.RuleFinding)
				if ok && reflect.DeepEqual(finding, expected) {
					severity := opsdoctor.SeverityWarn
					if expected.Severity == singboxvalidation.RuleSeverityError {
						severity = opsdoctor.SeverityError
					}
					if item.Severity != severity {
						t.Fatal("Doctor changed the owner severity")
					}
					found = true
				}
			}
			if !found {
				t.Fatalf("Doctor lost owner finding %#v", expected)
			}
		}
		encoded, _ := json.Marshal(report)
		if strings.Contains(string(encoded), "secret-fixture-marker") || strings.Contains(string(encoded), "password-fixture-marker") {
			t.Fatal("Doctor report leaked untrusted fields or values")
		}
		stored, err := (&SettingService{}).GetConfig()
		if err != nil || stored != payload {
			t.Fatalf("Doctor mutated historical storage: %v", err)
		}
	}
}
