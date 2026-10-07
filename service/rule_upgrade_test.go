package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	validation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
)

func TestStoredRuleUpgradeProjectionAndDoctorPreserveSource(t *testing.T) {
	initDoctorTestDB(t)
	runtime := NewRuntimeWithCoreProvider(nil)
	replaceDefaultRuntimeForTest(t, runtime)
	store := NewSingBoxBaseConfigStore(nil)
	for _, name := range []string{"route-and", "dns-or", "route-conflicting", "dns-action-only"} {
		original, err := os.ReadFile(filepath.Join("..", "internal", "singbox", "validation", "testdata", "nested_rules_v2026.3.3", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		// Exact old stored bytes, restored/imported before target acceptance exists.
		if err := dbsqlite.DB().Where("key = ?", "config").Assign(model.Setting{Value: string(original)}).FirstOrCreate(&model.Setting{Key: "config"}).Error; err != nil {
			t.Fatal(err)
		}
		expected, upgradeErr := validation.PrepareRuleUpgrade(original)
		projection, err := NewSingBoxConfigBuilder(runtime).BuildProjectionFromDB(dbsqlite.DB(), "")
		if (err != nil) != (upgradeErr != nil) {
			t.Fatal("builder diverged from upgrade owner", err, upgradeErr)
		}
		if !reflect.DeepEqual(projection.RuleCompatibility, expected.Findings) {
			t.Fatal("builder lost semantic diagnostics")
		}
		if err == nil {
			if bytes.Equal(projection.Config, original) {
				t.Fatal("legacy actions reached runtime")
			}
			if err := validation.ValidateConfig(projection.Config); err != nil {
				t.Fatal(err)
			}
			// A complete-candidate validation failure cannot rewrite the original DB.
			var document map[string]json.RawMessage
			_ = json.Unmarshal(projection.Config, &document)
			document["inbounds"] = json.RawMessage(`[{"type":"not-a-registered-inbound","tag":"fault"}]`)
			failed, _ := json.Marshal(document)
			if validation.ValidateConfig(failed) == nil {
				t.Fatal("failure injection did not reach constructor/decoder boundary")
			}
			// Construction succeeds but actual startup fails at a host-owned occupied
			// listener. No running generation or half-transformed storage escapes.
			occupied, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := occupied.Addr().(*net.TCPAddr).Port
			document["inbounds"], _ = json.Marshal([]map[string]any{{"type": "http", "tag": "fault", "listen": "127.0.0.1", "listen_port": port}})
			failed, _ = json.Marshal(document)
			if err := validation.ValidateConfig(failed); err != nil {
				_ = occupied.Close()
				t.Fatal("startup fixture must pass isolated construction", err)
			}
			candidateCore := coreruntime.NewCore()
			startErr := candidateCore.Start(failed)
			_ = candidateCore.Stop()
			_ = occupied.Close()
			if startErr == nil {
				t.Fatal("occupied listener did not fail candidate startup")
			}
			// A desired-state canonical save in an existing transaction is reversible.
			tx := dbsqlite.DB().Begin()
			if err := store.Save(tx, expected.Candidate); err != nil {
				t.Fatal(err)
			}
			if err := tx.Rollback().Error; err != nil {
				t.Fatal(err)
			}
		}
		report := (&DoctorService{Runtime: runtime}).Run("fixture.example")
		for _, finding := range expected.Findings {
			found := false
			for _, item := range report.Items {
				if actual, ok := item.Details.(validation.RuleFinding); ok && reflect.DeepEqual(actual, finding) {
					found = true
				}
			}
			if !found {
				t.Fatal("Doctor uses a different upgrade contract", finding)
			}
		}
		after, err := store.Get()
		if err != nil || after != string(original) {
			t.Fatal("projection/Doctor/failed startup/rollback changed old storage", err)
		}
		// Raw/custom/new-save paths reject even the automatically projected shape.
		if _, err := NewSingBoxConfigBuilder(runtime).Build(string(original)); err == nil {
			t.Fatal("new custom JSON bypassed strict acceptance")
		}
		var typed *validation.RuleConditionError
		if err := store.Set(string(original)); !errors.As(err, &typed) || typed.Finding.Code != "nested_rule_action" {
			t.Fatal("new save did not reject raw old state", err)
		}
	}
}
