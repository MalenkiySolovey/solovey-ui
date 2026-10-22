package datalifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	wire "github.com/MalenkiySolovey/solovey-ui/contracts/management/v1"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestManagementContractReplayAgreesWithActualRestoreOwner(t *testing.T) {
	data, err := os.ReadFile("../../contracts/management/v1/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var set struct {
		Cases []struct {
			ID    string
			Now   int64
			Steps []struct{ Command wire.Command }
		}
	}
	if err := json.Unmarshal(data, &set); err != nil {
		t.Fatal(err)
	}
	selected := map[string]bool{"same-command-same-payload-replay": true, "ambiguous-delivery-retry": true, "same-id-different-payload": true}
	count := 0
	for _, v := range set.Cases {
		if !selected[v.ID] {
			continue
		}
		count++
		t.Run(v.ID, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "restore-proof.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			pool.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = pool.Close() })
			if err := db.AutoMigrate(&model.DataLifecycleOperation{}, &model.DataLifecycleJournal{}); err != nil {
				t.Fatal(err)
			}
			first := v.Steps[0].Command
			operation := model.DataLifecycleOperation{OperationID: "data-operation:" + semanticDigest(struct{ Key, Revision string }{first.CommandID, string(*first.ExpectedRevision)})[:48], IdempotencyKey: first.CommandID, Kind: "RESTORE", State: "RESTORING", OwnerID: "core", ExpectedRevision: string(*first.ExpectedRevision), Revision: 2, ManifestDigest: strings.Repeat("b", 64), CreatedAt: v.Now, UpdatedAt: v.Now}
			if err := db.Create(&operation).Error; err != nil {
				t.Fatal(err)
			}
			manager := &Manager{DB: func() *gorm.DB { return db }, Now: func() time.Time { return time.Unix(v.Now, 0) }, Root: t.TempDir(), RestoreRoot: t.TempDir()}
			var before int64
			if err := db.Raw("SELECT total_changes()").Scan(&before).Error; err != nil {
				t.Fatal(err)
			}
			for index, step := range v.Steps {
				request := RestoreRequest{ExpectedRehearsalRevision: string(*step.Command.ExpectedRevision), IdempotencyKey: step.Command.CommandID, Acknowledged: true, Source: strings.NewReader("must not rehearse an already recorded command")}
				request.Confirmation = RestoreConfirmation(request.ExpectedRehearsalRevision)
				request.Authorize = func(_ context.Context, invocation actions.Invocation) (actions.Decision, error) {
					return actions.Decision{Allowed: true, Actor: "panel-account:fixture", Permission: actions.PermissionBackupRestore, Invocation: invocation}, nil
				}
				result, _, err := manager.ExecuteRestore(t.Context(), request)
				if index == 1 && v.ID == "same-id-different-payload" {
					if !errors.Is(err, ErrPreviewChanged) {
						t.Fatalf("changed replay=%v", err)
					}
				} else if err != nil || result.OperationID != operation.OperationID || result.Revision != operation.Revision {
					t.Fatalf("replay=%+v err=%v", result, err)
				}
				request.Authorize = nil
				if _, _, err := manager.ExecuteRestore(t.Context(), request); !errors.Is(err, actions.ErrDenied) {
					t.Fatalf("idempotency restored missing authority: %v", err)
				}
			}
			var after int64
			if err := db.Raw("SELECT total_changes()").Scan(&after).Error; err != nil || before != after {
				t.Fatalf("replay wrote owner database: %d -> %d err=%v", before, after, err)
			}
		})
	}
	if count != len(selected) {
		t.Fatal("missing actual restore replay vectors")
	}
}
