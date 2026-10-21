//go:build !minimal

package importxui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/components/import-xui/database/mapping"
	"github.com/MalenkiySolovey/solovey-ui/components/import-xui/database/source"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestImportUnknownFieldsBlockDependenciesAndCannotBeApprovedByBrowser(t *testing.T) {
	for _, mutation := range []string{
		`UPDATE inbounds SET settings = '{"clients":[{"id":"00000000-0000-4000-8000-000000000001","email":"alice"}],"unmappedAuth":"private-fixture-value"}'`,
		`UPDATE inbounds SET stream_settings = '{"network":"splithttp","security":"none"}'`,
		`ALTER TABLE inbounds ADD COLUMN unknown_security_extension TEXT`,
		`ALTER TABLE inbounds ADD COLUMN "port protocol" TEXT`,
		`ALTER TABLE inbounds ADD COLUMN allocate TEXT; UPDATE inbounds SET allocate = '{"strategy":"random"}'`,
	} {
		t.Run(strings.Split(mutation, " ")[0]+":"+strings.Split(mutation, " ")[3], func(t *testing.T) {
			initPlanExtraMainDB(t)
			baseline := tableCounts(t, "inbounds", "tls", "clients")
			path := createPlanExtraSource(t, []planExtraInbound{validPlanExtraInbound()})
			foreign, err := gorm.Open(gormsqlite.Open(path), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			// TLS and its users must not survive a blocked inbound as an
			// apparently complete partial import.
			if err := foreign.Exec("UPDATE inbounds SET stream_settings = ?", runtimeTLSFixtureStream(t, "source.fixture.example")).Error; err != nil {
				t.Fatal(err)
			}
			for _, statement := range strings.Split(mutation, ";") {
				if err := foreign.Exec(statement).Error; err != nil {
					t.Fatal(err)
				}
			}
			handle, err := foreign.DB()
			if err != nil {
				t.Fatal(err)
			}
			if err := handle.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := source.Hash(path)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := Plan(path, PlanOptions{})
			if err != nil {
				t.Fatal(err)
			}
			inboundIndex, blocked := -1, map[string]bool{}
			for index, item := range plan.Items {
				if item.Kind != KindInbound && item.Kind != KindTLS && item.Kind != KindClient {
					continue
				}
				if item.Action != ActionSkip || string(item.PreviewJSON) != "null" || !mapping.Blocking(item.Fields) {
					t.Fatalf("unsafe partial %s item: action=%s classification=%s", item.Kind, item.Action, item.Classification)
				}
				blocked[item.Kind] = true
				if item.Kind == KindInbound {
					inboundIndex = index
				}
				diagnostics, err := json.Marshal(item.Fields)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(diagnostics), "private-fixture-value") {
					t.Fatal("classification exposed a source value")
				}
			}
			if inboundIndex < 0 || !blocked[KindClient] {
				t.Fatal("source dependency classification missing")
			}
			approved := cloneMigrationPlan(*plan)
			approved.Items[inboundIndex].Action = ActionCreate
			approved.Items[inboundIndex].Fields = nil
			approved.Items[inboundIndex].Classification = mapping.ExactSupported
			if _, err := Apply(path, approved, ApplyOptions{SkipBackup: true}); !errors.Is(err, ErrPlanInvalid) {
				t.Fatalf("browser approved a blocked field: %v", err)
			}
			if _, err := Apply(path, *plan, ApplyOptions{SkipBackup: true, SkipAudit: true}); err != nil {
				t.Fatal(err)
			}
			for _, table := range []string{"inbounds", "tls", "clients"} {
				if destinationRowCount(t, table) != baseline[table] {
					t.Fatalf("blocked import wrote %s", table)
				}
			}
			after, err := source.Hash(path)
			if err != nil || before != after {
				t.Fatal("planning/apply changed immutable source", err)
			}
		})
	}
}

func TestImportChangeJobOutcomeComesFromAtomicOwnerAuditAfterRestart(t *testing.T) {
	initPlanExtraMainDB(t)
	path := createPlanExtraSource(t, []planExtraInbound{validPlanExtraInbound()})
	plan, err := Plan(path, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var references []operationcoordination.Reference
	report, err := Apply(path, *plan, ApplyOptions{SkipBackup: true, OperationID: "import:owner-fixture", Actor: "fixture-operator", OnProgress: func(p Progress) {
		if p.Reference == nil {
			t.Fatal("progress lost operation reference")
		}
		references = append(references, *p.Reference)
	}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Change == nil || report.Job == nil || report.Change.Outcome != operationcoordination.OutcomeCommitted || report.Job.Outcome != operationcoordination.OutcomeCommitted {
		t.Fatal("committed outcome unavailable")
	}
	if len(references) == 0 || references[0] != report.Change.Reference || report.Job.Reference != report.Change.Reference {
		t.Fatal("Change, Job and progress disagree")
	}
	if report.Job.Progress != nil || report.Job.CancelSupported || report.Job.CancelRequested != nil || report.Job.CriticalSection {
		t.Fatal("invented committed job capabilities")
	}
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.Init(filepath.Join(os.Getenv("SUI_DB_FOLDER"), "s-ui.db")); err != nil {
		t.Fatal(err)
	}
	var events []model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "panel_import").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	entries := ProjectHistory(events)
	if len(entries) != 1 || entries[0].Change == nil || entries[0].Job == nil || entries[0].Actor != "fixture-operator" {
		t.Fatal("committed owner history missing")
	}
	if !reflect.DeepEqual(entries[0].Change, report.Change) || !reflect.DeepEqual(entries[0].Job, report.Job) {
		t.Fatal("restart projected a different outcome")
	}
	legacy := ProjectHistory([]model.AuditEvent{{Event: "panel_import", Details: json.RawMessage(`{"summary":{}}`)}})
	if legacy[0].Change != nil || legacy[0].Job != nil {
		t.Fatal("invented durable operation for legacy history")
	}
}

func TestImportAuditFailureRollsBackDestinationAndNeverReportsCommitted(t *testing.T) {
	initPlanExtraMainDB(t)
	path := createPlanExtraSource(t, []planExtraInbound{validPlanExtraInbound()})
	plan, err := Plan(path, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Exec(`CREATE TRIGGER reject_fixture_import_audit BEFORE INSERT ON audit_events WHEN NEW.event = 'panel_import' BEGIN SELECT RAISE(ABORT, 'fixture audit failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	report, err := Apply(path, *plan, ApplyOptions{SkipBackup: true, OperationID: "import:failed-fixture"})
	if err == nil {
		t.Fatal("failed owner audit was accepted")
	}
	if report.Change == nil || report.Job == nil || report.Change.Outcome != operationcoordination.OutcomeFailed || report.Job.Outcome != operationcoordination.OutcomeFailed {
		t.Fatal("failure retained committed projection")
	}
	for _, table := range []string{"inbounds", "clients", "audit_events"} {
		if destinationRowCount(t, table) != 0 {
			t.Fatalf("failed atomic import wrote %s", table)
		}
	}
}

func TestImportMembershipMergeAndInboundReplacementPreserveAccessAndLifetime(t *testing.T) {
	initPlanExtraMainDB(t)
	path := createPlanExtraSource(t, []planExtraInbound{validPlanExtraInbound()})
	plan, err := Plan(path, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(path, *plan, ApplyOptions{SkipBackup: true, SkipAudit: true}); err != nil {
		t.Fatal(err)
	}
	db := dbsqlite.DB()
	var user model.Client
	if err := db.Where("name = ?", "alice").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&user).Updates(map[string]any{"enable": false, "access_disable_reason": model.ClientDisabledManual, "up": 7, "down": 9, "total_up": 70, "total_down": 90}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&user, user.Id).Error; err != nil {
		t.Fatal(err)
	}
	before := user
	var old model.Inbound
	if err := db.Where("tag = ?", "test-vless").First(&old).Error; err != nil {
		t.Fatal(err)
	}
	plan, err = Plan(path, PlanOptions{Strategy: StrategyReplace})
	if err != nil {
		t.Fatal(err)
	}
	for index := range plan.Items {
		if plan.Items[index].Kind == KindClient {
			plan.Items[index].Action = ActionMerge
		}
	}
	if _, err := Apply(path, *plan, ApplyOptions{SkipBackup: true, SkipAudit: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&user, user.Id).Error; err != nil {
		t.Fatal(err)
	}
	var replacement model.Inbound
	if err := db.Where("tag = ?", "test-vless").First(&replacement).Error; err != nil {
		t.Fatal(err)
	}
	if replacement.Id == old.Id {
		t.Fatal("replacement did not recreate canonical inbound identity")
	}
	var bindings []uint
	if err := json.Unmarshal(user.Inbounds, &bindings); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bindings, []uint{replacement.Id}) {
		t.Fatal("canonical membership retained retired inbound")
	}
	user.Inbounds = before.Inbounds
	if !reflect.DeepEqual(user, before) {
		t.Fatal("membership merge changed access, credentials, subscription, counters or lifetime")
	}
}
