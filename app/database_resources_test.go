package app

import (
	"bytes"
	"context"
	"errors"
	"testing"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	dbhooks "github.com/MalenkiySolovey/solovey-ui/database/hooks"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	datalifecycle "github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
	"gorm.io/gorm"
)

func TestLogicalRestoreRebindsApplicationResourceOwners(t *testing.T) {
	for _, reject := range []bool{false, true} {
		name := "applied"
		if reject {
			name = "rejected_owner_rebind"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("SUI_DB_FOLDER", t.TempDir())
			application := NewApp()
			if err := application.Init(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(application.Stop)
			dbbackup.SetSendSighupHook(func() error { return nil })
			t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })
			// SSH socket observation is independently qualified by the native
			// contract. Two static external resources isolate the DB lifecycle.
			stopSSH, err := hostresources.Register(restoreSSHResources{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(stopSSH)
			checkResources := func() {
				t.Helper()
				snapshot := hostresources.Default.Refresh(context.Background())
				if len(snapshot.Errors) != 0 || len(snapshot.Resources) != 4 {
					t.Fatalf("resource inventory: count=%d errors=%v", len(snapshot.Resources), snapshot.Errors)
				}
				ids := map[string]bool{}
				for _, resource := range snapshot.Resources {
					ids[resource.ID] = true
				}
				for _, id := range []string{"core:panel:web", "core:subscription:default", "fixture:ssh:ipv4", "fixture:ssh:ipv6"} {
					if !ids[id] {
						t.Fatalf("required resource %s absent", id)
					}
				}
			}
			checkResources()
			oldDB, oldControl := dbsqlite.DB(), application.configService.CoreInboundControl()
			if err := oldDB.Model(&model.Setting{}).Where("key = ?", "trafficAge").Update("value", "32").Error; err != nil {
				t.Fatal(err)
			}
			backup, err := dbbackup.Export("")
			if err != nil {
				t.Fatal(err)
			}
			if err := oldDB.Model(&model.Setting{}).Where("key = ?", "trafficAge").Update("value", "33").Error; err != nil {
				t.Fatal(err)
			}
			if reject {
				failed := false
				dbhooks.RegisterContextResetHook("app.zz_rebind_rejection", func(context.Context) error {
					if !failed {
						failed = true
						return errors.New("injected mandatory owner rebind failure")
					}
					return nil
				})
				t.Cleanup(func() { dbhooks.RegisterContextResetHook("app.zz_rebind_rejection", nil) })
			}
			rehearsal, err := dbbackup.Rehearse(context.Background(), bytes.NewReader(backup))
			if err != nil || !rehearsal.Possible {
				t.Fatalf("rehearsal: possible=%v err=%v", rehearsal.Possible, err)
			}
			manager := datalifecycle.NewManager()
			// Host resource pressure is independently qualified. This fixture
			// admits the logical transaction without starting host watchers.
			manager.Admit = func(string) bool { return true }
			accepted := 0
			operation, result, err := manager.ExecuteRestore(context.Background(), datalifecycle.RestoreRequest{
				ExpectedRehearsalRevision: rehearsal.Revision, IdempotencyKey: "restore-resource-generation",
				Confirmation: datalifecycle.RestoreConfirmation(rehearsal.Revision), Acknowledged: true, Source: bytes.NewReader(backup),
				OnAccepted: func() { accepted++ },
			})
			if reject {
				if err == nil || operation.State == "APPLIED" {
					t.Fatal("failed owner rebind was accepted")
				}
			} else if err != nil || operation.State != "APPLIED" || result.RestartPending || result.RecoveryCleanupPending {
				t.Fatalf("restore: state=%s restart=%v cleanup=%v err=%v", operation.State, result.RestartPending, result.RecoveryCleanupPending, err)
			}
			wantAccepted := 1
			if reject {
				wantAccepted = 0
			}
			if accepted != wantAccepted {
				t.Fatalf("accepted callbacks=%d want=%d", accepted, wantAccepted)
			}
			if dbsqlite.DB() == oldDB {
				t.Fatal("restore did not replace the database object")
			}
			if _, err := oldControl.ListSnapshots(context.Background(), 1); err == nil {
				t.Fatal("old-generation control still reads the closed database")
			}
			current := application.configService.CoreInboundControl()
			if current == oldControl {
				t.Fatal("current owner retained the old generation")
			}
			if _, err := current.ListSnapshots(context.Background(), 1); err != nil {
				t.Fatal(err)
			}
			var canary model.Setting
			if err := dbsqlite.DB().First(&canary, "key = ?", "trafficAge").Error; err != nil {
				t.Fatal(err)
			}
			want := "32"
			if reject {
				want = "33"
			}
			if canary.Value != want {
				t.Fatalf("canary=%s want=%s", canary.Value, want)
			}
			// Mandatory rebinding has completed before the async restart. Its
			// ordinary unregister/register cycle must retain the new owner.
			checkResources()
			application.stopResourceRegistrations()
			if err := application.registerResources(); err != nil {
				t.Fatal(err)
			}
			checkResources()
		})
	}
}

func TestRestoreAcceptanceSignalSurvivesPruneErrorAndIsNotReplayed(t *testing.T) {
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	application := NewApp()
	if err := application.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(application.Stop)
	dbbackup.SetSendSighupHook(func() error { return errors.New("restart deferred") })
	t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })
	backup, err := dbbackup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	rehearsal, err := dbbackup.Rehearse(t.Context(), bytes.NewReader(backup))
	if err != nil || !rehearsal.Possible {
		t.Fatal("rehearsal failed", err)
	}
	manager := datalifecycle.NewManager()
	manager.Admit = func(string) bool { return true }
	accepted, failPrune := 0, false
	manager.DB = func() *gorm.DB {
		if failPrune {
			return nil
		}
		return dbsqlite.DB()
	}
	request := datalifecycle.RestoreRequest{
		ExpectedRehearsalRevision: rehearsal.Revision, IdempotencyKey: "restore-accepted-before-prune",
		Confirmation: datalifecycle.RestoreConfirmation(rehearsal.Revision), Acknowledged: true, Source: bytes.NewReader(backup),
		OnAccepted: func() { accepted++; failPrune = true },
	}
	operation, result, err := manager.ExecuteRestore(t.Context(), request)
	if err == nil || operation.State != "APPLIED" || accepted != 1 || !result.RestartPending {
		t.Fatalf("accepted restore state=%s callbacks=%d restart=%v error=%v", operation.State, accepted, result.RestartPending, err)
	}
	failPrune = false
	var stored model.DataLifecycleOperation
	if err := dbsqlite.DB().First(&stored, "operation_id = ?", operation.OperationID).Error; err != nil || stored.State != "APPLIED" {
		t.Fatal("acceptance was not durable", err)
	}
	request.Source = bytes.NewReader(backup)
	replayed, _, err := manager.ExecuteRestore(t.Context(), request)
	if err != nil || replayed.OperationID != operation.OperationID || accepted != 1 {
		t.Fatalf("idempotent restore callbacks=%d error=%v", accepted, err)
	}
}

type restoreSSHResources struct{}

func (restoreSSHResources) Owner() string { return "fixture:ssh" }
func (restoreSSHResources) ListProtectableResources(context.Context) ([]hostresources.ProtectableResource, error) {
	return []hostresources.ProtectableResource{
		{ID: "fixture:ssh:ipv4", Kind: "ssh", Owner: "fixture:ssh", Source: "ssh", Protocol: "tcp", Listen: "192.0.2.10", Port: 22},
		{ID: "fixture:ssh:ipv6", Kind: "ssh", Owner: "fixture:ssh", Source: "ssh", Protocol: "tcp", Listen: "2001:db8::10", Port: 22},
	}, nil
}
