package backup

import (
	"bytes"
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/hooks"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func waitRestoreAdmissionFreeze(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		_, release, err := dbsqlite.AcquireOperation(ctx)
		if errors.Is(err, dbsqlite.ErrMaintenance) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		release()
		runtime.Gosched()
	}
}

func TestExportFinishesAdmittedSnapshotAcrossQueryGap(t *testing.T) {
	initRestoreRecoveryPublicationDB(t)
	original := dbsqlite.DB()
	entered, proceed := make(chan struct{}), make(chan struct{})
	var enterOnce, resumeOnce sync.Once
	resume := func() { resumeOnce.Do(func() { close(proceed) }) }
	t.Cleanup(resume)
	const callback = "test:export-operation-gap"
	if err := original.Callback().Query().After("gorm:after_query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "settings" {
			enterOnce.Do(func() { close(entered); <-proceed })
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = original.Callback().Query().Remove(callback) })
	type exportResult struct {
		path    string
		cleanup func()
		err     error
	}
	exported := make(chan exportResult, 1)
	go func() {
		path, cleanup, err := PrepareExportContext(context.Background(), "")
		exported <- exportResult{path, cleanup, err}
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type maintenanceResult struct {
		owner *dbsqlite.Maintenance
		err   error
	}
	maintained := make(chan maintenanceResult, 1)
	go func() { owner, err := dbsqlite.BeginMaintenance(ctx); maintained <- maintenanceResult{owner, err} }()
	waitRestoreAdmissionFreeze(t)
	select {
	case got := <-maintained:
		if got.owner != nil {
			got.owner.End()
		}
		t.Fatalf("maintenance crossed export query gap: %v", got.err)
	default:
	}
	if _, _, err := PrepareExportContext(ctx, ""); !errors.Is(err, dbsqlite.ErrMaintenance) {
		t.Fatalf("late export admission: %v", err)
	}
	resume()
	var result exportResult
	select {
	case result = <-exported:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if result.err != nil {
		t.Fatal(result.err)
	}
	defer result.cleanup()
	if dbsqlite.DB() != original {
		t.Fatal("export changed live generation")
	}
	var maintainedResult maintenanceResult
	select {
	case maintainedResult = <-maintained:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if maintainedResult.err != nil {
		t.Fatal(maintainedResult.err)
	}
	defer maintainedResult.owner.End()
	backup, err := gorm.Open(gormsqlite.Open(result.path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := backup.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var setting model.Setting
	if err := backup.Where("key = ?", "recovery-restore-recovery").First(&setting).Error; err != nil || setting.Value != "published" {
		t.Fatalf("admitted export snapshot: %v", err)
	}
}

func TestRestoreCandidateStaysPrivateThroughAcceptanceOrExactAbort(t *testing.T) {
	for _, decision := range []string{"accept", "abort"} {
		t.Run(decision, func(t *testing.T) {
			initRestoreRecoveryPublicationDB(t)
			SetSendSighupHook(func() error { return nil })
			t.Cleanup(func() { SetSendSighupHook(nil) })
			const marker = "lifetime-restore-marker"
			if err := dbsqlite.DB().Create(&model.Setting{Key: marker, Value: "snapshot"}).Error; err != nil {
				t.Fatal(err)
			}
			input, err := Export("")
			if err != nil {
				t.Fatal(err)
			}
			if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", marker).Update("value", "exact-live").Error; err != nil {
				t.Fatal(err)
			}
			original := dbsqlite.DB()
			var resetValues []string
			const hook = "test.restore.private-rebind"
			hooks.RegisterContextResetHook(hook, func(ctx context.Context) error {
				if !dbsqlite.IsMaintenanceContext(ctx) {
					return errors.New("reset received no private authority")
				}
				var setting model.Setting
				if err := dbsqlite.DB().WithContext(ctx).Where("key = ?", marker).First(&setting).Error; err != nil {
					return err
				}
				resetValues = append(resetValues, setting.Value)
				return nil
			})
			t.Cleanup(func() { hooks.RegisterContextResetHook(hook, nil) })
			request, cancelRequest := context.WithCancel(context.Background())
			defer cancelRequest()
			request, release, err := dbsqlite.AcquireOperation(request)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			result, err := RestoreContextDetailed(request, bytes.NewReader(input))
			if err != nil {
				t.Fatal(err)
			}
			private := result.DatabaseContext(context.Background())
			candidate := dbsqlite.DB()
			if candidate == nil || candidate == original || !dbsqlite.IsMaintenanceContext(private) {
				t.Fatal("candidate private lifetime was not retained")
			}
			if _, _, err := dbsqlite.AcquireOperation(context.Background()); !errors.Is(err, dbsqlite.ErrMaintenance) {
				t.Fatalf("candidate admitted ordinary operation: %v", err)
			}
			var setting model.Setting
			if err := candidate.Where("key = ?", marker).First(&setting).Error; !errors.Is(err, dbsqlite.ErrMaintenance) {
				t.Fatalf("candidate public handle authority: %v", err)
			}
			if err := candidate.WithContext(private).Create(&model.Setting{Key: "candidate-operation-authority", Value: "persisted"}).Error; err != nil {
				t.Fatal(err)
			}
			if decision == "accept" {
				cleanup, restart, err := CompletePendingRestore(request)
				if err != nil || cleanup || restart {
					t.Fatalf("acceptance pending=%v/%v err=%v", cleanup, restart, err)
				}
				if err := dbsqlite.DB().WithContext(request).Where("key = ?", marker).First(&setting).Error; err != nil || setting.Value != "snapshot" {
					t.Fatalf("readmitted response: %v", err)
				}
				if len(resetValues) != 1 || resetValues[0] != "snapshot" {
					t.Fatalf("candidate rebind=%v", resetValues)
				}
			} else {
				cancelRequest() // rollback must outlive the rejected request
				if err := AbortPendingRestore(); err != nil {
					t.Fatal(err)
				}
				if err := dbsqlite.DB().Where("key = ?", marker).First(&setting).Error; err != nil || setting.Value != "exact-live" {
					t.Fatalf("exact recovery: %v", err)
				}
				if err := dbsqlite.DB().Where("key = ?", "candidate-operation-authority").First(&model.Setting{}).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
					t.Fatalf("rejected candidate authority survived: %v", err)
				}
				if len(resetValues) != 2 || resetValues[0] != "snapshot" || resetValues[1] != "exact-live" {
					t.Fatalf("candidate/fallback rebind=%v", resetValues)
				}
			}
			if err := dbsqlite.DB().WithContext(private).Raw("SELECT 1").Scan(new(int)).Error; !errors.Is(err, dbsqlite.ErrRetired) {
				t.Fatalf("finished private capability remained usable: %v", err)
			}
			if err := original.Raw("SELECT 1").Scan(new(int)).Error; err == nil {
				t.Fatal("original stale handle regained authority")
			}
			release()
			_, release, err = dbsqlite.AcquireOperation(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			release()
		})
	}
}

func TestRestoreFatalFallbackRebindRetainsMaintenance(t *testing.T) {
	initRestoreRecoveryPublicationDB(t)
	input, err := Export("")
	if err != nil {
		t.Fatal(err)
	}
	const hook = "test.restore.reject-all-rebinds"
	hooks.RegisterContextResetHook(hook, func(context.Context) error { return errors.New("injected owner rebind failure") })
	t.Cleanup(func() { hooks.RegisterContextResetHook(hook, nil) })
	result, err := RestoreContextDetailed(context.Background(), bytes.NewReader(input))
	if err == nil {
		t.Fatal("unrebound fallback reported successful restore")
	}
	if !dbsqlite.IsMaintenanceContext(result.DatabaseContext(context.Background())) {
		t.Fatal("fatal rebind released private lifecycle")
	}
	if _, _, err := dbsqlite.AcquireOperation(context.Background()); !errors.Is(err, dbsqlite.ErrMaintenance) {
		t.Fatalf("unrebound fallback admitted work: %v", err)
	}
	if dbsqlite.DB() == nil {
		t.Fatal("fatal rebind exposed a nil public database")
	}
	if err := dbsqlite.DB().Raw("SELECT 1").Scan(new(int)).Error; !errors.Is(err, dbsqlite.ErrMaintenance) {
		t.Fatalf("unrebound fallback accepted SQL: %v", err)
	}
	// Explicit terminal close is test teardown, not a recovered-health claim.
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
}
