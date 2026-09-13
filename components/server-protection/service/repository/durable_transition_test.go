package repository

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	sqlite3 "github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

func TestDurableTransitionReservesWriterBeforeAuthorityRead(t *testing.T) {
	if err := dbsqlite.Init(filepath.Join(t.TempDir(), "production.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	db := dbsqlite.DB()
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	competitor, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer competitor.Close()
	// Diagnostic probe only: don't wait on a writer we deliberately keep held.
	// The owner connection still uses the production 10000ms timeout.
	if _, err := competitor.ExecContext(t.Context(), "PRAGMA busy_timeout=0"); err != nil {
		t.Fatal(err)
	}
	defer competitor.ExecContext(t.Context(), "PRAGMA busy_timeout=10000")
	repo := New(db)
	rollback := errors.New("rollback probe")
	for _, abort := range []bool{false, true} {
		err = repo.durableTransition(t.Context(), "test_reservation", func(tx *gorm.DB) error {
			var count int64
			if err := tx.Model(&OperationLockModel{}).Count(&count).Error; err != nil {
				return err
			}
			_, probeErr := competitor.ExecContext(t.Context(), "UPDATE server_protection_operation_locks SET revision=revision WHERE 0")
			var driver sqlite3.Error
			if !errors.As(probeErr, &driver) || driver.Code != sqlite3.ErrBusy || driver.ExtendedCode != sqlite3.ErrNoExtended(sqlite3.ErrBusy) {
				t.Fatalf("writer was not reserved before read: %v", probeErr)
			}
			if abort {
				return rollback
			}
			return nil
		})
		if abort && !errors.Is(err, rollback) || !abort && err != nil {
			t.Fatal(err)
		}
		if _, err := competitor.ExecContext(t.Context(), "UPDATE server_protection_operation_locks SET revision=revision WHERE 0"); err != nil {
			t.Fatalf("writer not released after commit/rollback: %v", err)
		}
	}
	t.Log("real SQLite probe: competing writer BUSY primary=5 extended=5 before owner read; released on commit and rollback; no rows changed")
}

func TestPersistenceClassificationKeepsDriverCausePrivate(t *testing.T) {
	for _, extended := range []sqlite3.ErrNoExtended{sqlite3.ErrBusySnapshot, sqlite3.ErrNoExtended(sqlite3.ErrBusy)} {
		err := persistenceError("firewall_runtime", "durable_write_transaction", sqlite3.Error{Code: sqlite3.ErrBusy, ExtendedCode: extended})
		var driver sqlite3.Error
		if !errors.As(err, &driver) || driver.ExtendedCode != extended {
			t.Fatal("lost driver classification")
		}
		var classified *PersistenceError
		if !errors.As(err, &classified) || classified.Primary != 5 || classified.Extended != int(extended) {
			t.Fatal("wrong classification")
		}
	}
	err := persistenceError("firewall_runtime", "durable_write_transaction", errors.New("private SQL parameter sentinel"))
	if strings.Contains(err.Error(), "sentinel") {
		t.Fatal("private cause exposed")
	}
}
