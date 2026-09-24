package firewall

import (
	"database/sql"
	"errors"
	"runtime"
	"strings"
	"testing"

	repository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	auditsvc "github.com/MalenkiySolovey/solovey-ui/service/audit"
	sqlite3 "github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

func TestRuntimeSQLiteConcurrentAuditLifecycle(t *testing.T) {
	assertCurrentSSHProductionLifecycle(t, true, "", "sqlite_concurrent_audit")
}

func TestRuntimeSQLiteStuckR16Upgrade(t *testing.T) {
	assertCurrentSSHProductionLifecycle(t, true, "", "sqlite_stuck_r16")
}

func TestRuntimeSQLitePersistenceFailureBeforeMutation(t *testing.T) {
	assertCurrentSSHProductionLifecycle(t, true, "", "sqlite_readonly_fence")
}

func runtimeReadonlyFence(t *testing.T, db *gorm.DB) func() {
	t.Helper()
	fired := false
	var failedTx *sql.Tx
	var failure error
	if err := db.Callback().Update().Before("gorm:update").Register("runtime:readonly", func(tx *gorm.DB) {
		composition, ok := tx.Statement.Dest.(*repository.FirewallCompositionModel)
		if fired || !ok || composition.Runtime.State != "RESTORING_RUNTIME" || !runtimeFenceStack() {
			return
		}
		fired = true
		failedTx, _ = tx.Statement.ConnPool.(*sql.Tx)
		if failedTx == nil {
			t.Fatal("runtime write is not transactional")
		}
		if _, err := failedTx.ExecContext(t.Context(), "PRAGMA query_only=ON"); err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().After("gorm:update").Register("runtime:readonly_reset", func(tx *gorm.DB) {
		transaction, _ := tx.Statement.ConnPool.(*sql.Tx)
		if failedTx == nil || transaction != failedTx {
			return
		}
		failure = tx.Error
		if _, err := failedTx.ExecContext(t.Context(), "PRAGMA query_only=OFF"); err != nil {
			t.Error(err)
		}
		failedTx = nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Update().Remove("runtime:readonly"); err != nil {
			t.Errorf("remove readonly callback: %v", err)
		}
		if err := db.Callback().Update().Remove("runtime:readonly_reset"); err != nil {
			t.Errorf("remove readonly reset callback: %v", err)
		}
	})
	return func() {
		var code sqlite3.Error
		if !fired || !errors.As(failure, &code) || code.Code != sqlite3.ErrReadonly {
			t.Fatalf("real SQLite persistence failure not observed: %v", failure)
		}
		t.Logf("real pre-mutation persistence failure primary=%d extended=%d", code.Code, code.ExtendedCode)
	}
}

func productionStartupSQLite(t *testing.T) func(string) (*gorm.DB, error) {
	return func(path string) (*gorm.DB, error) {
		if err := dbsqlite.Init(path); err != nil {
			return nil, err
		}
		t.Cleanup(func() { _ = dbsqlite.Close() })
		return dbsqlite.DB(), nil
	}
}

// The audit writer reaches its SQL create boundary after the runtime owner has
// read authority in its reserved transaction. It must wait for that transaction
// to commit; it must not invalidate the reader's snapshot. No sleep schedules it.
func runtimeConcurrentAuditBarrier(t *testing.T, db *gorm.DB, operationID string) func() {
	t.Helper()
	var journal string
	var timeout int
	if err := db.Raw("PRAGMA journal_mode").Scan(&journal).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("PRAGMA busy_timeout").Scan(&timeout).Error; err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if journal != "wal" || timeout != 10000 || pool.Stats().MaxOpenConnections != 8 {
		t.Fatal("production SQLite configuration changed")
	}
	started, finished := make(chan *sql.Tx, 1), make(chan error, 1)
	fired := false
	if err := db.Callback().Create().Before("gorm:create").Register("runtime:audit_writer", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*[]model.AuditEvent); ok {
			writer, _ := tx.Statement.ConnPool.(*sql.Tx)
			started <- writer
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove("runtime:audit_writer"); err != nil {
			t.Errorf("remove audit writer callback: %v", err)
		}
	})
	if err := db.Callback().Query().After("gorm:query").Register("runtime:read_barrier", func(tx *gorm.DB) {
		operation, ok := tx.Statement.Dest.(*repository.OperationLockModel)
		reader, inTx := tx.Statement.ConnPool.(*sql.Tx)
		if fired || !ok || !inTx || tx.Error != nil || operation.OperationID != operationID || operation.State != "restoring_runtime" || !runtimeFenceStack() {
			return
		}
		fired = true
		go func() {
			finished <- auditsvc.WriteEventsContext(t.Context(), []model.AuditEvent{{Actor: "system", Event: "server_protection.helper", Resource: "server-protection", Severity: "info", Details: []byte("{}")}})
		}()
		select {
		case writer := <-started:
			if writer == nil || writer == reader {
				t.Error("independent writer transaction missing")
			}
		case <-t.Context().Done():
			t.Error("audit writer did not enter")
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove("runtime:read_barrier"); err != nil {
			t.Errorf("remove read barrier callback: %v", err)
		}
	})
	return func() {
		if !fired {
			t.Fatal("runtime fence barrier not reached")
		}
		select {
		case err := <-finished:
			if err != nil {
				t.Fatal(err)
			}
		case <-t.Context().Done():
			t.Fatal("audit writer did not finish")
		}
		t.Log("production WAL/10000ms/8 connections: runtime transaction and independent audit writer committed; full original-operation lifecycle continues")
	}
}

func runtimeFenceStack() bool {
	var pcs [48]uintptr
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs[:])])
	for {
		frame, more := frames.Next()
		if strings.Contains(frame.Function, "repository.(*Repository).UpdateFirewallRuntime") {
			return true
		}
		if !more {
			return false
		}
	}
}
