package repository

import (
	"context"
	"errors"
	"fmt"

	sqlite3 "github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

// durableTransition is restricted to short repository-owned DB transitions.
// SQLite's deferred BEGIN otherwise lets the validation SELECT establish a WAL
// snapshot that an independent audit writer can invalidate before our UPDATE.
// Reserve the writer without modifying rows, then read and write on the same
// *sql.Tx. Never call services, filesystem, helpers or health inside fn.
func (r *Repository) durableTransition(ctx context.Context, step string, fn func(*gorm.DB) error) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("UPDATE server_protection_operation_locks SET revision = revision WHERE 0").Error; err != nil {
			return err
		}
		return fn(tx)
	})
	return persistenceError(step, "durable_write_transaction", err)
}

// PersistenceError preserves the private driver cause but prints no SQL,
// parameters, session material or database contents. Step and role are fixed
// by repository call sites, never supplied by an API caller.
type PersistenceError struct {
	Step     string
	Role     string
	Primary  int
	Extended int
	cause    error
}

func (e *PersistenceError) Error() string {
	return fmt.Sprintf("server-protection persistence step=%s role=%s sqlite_primary=%d sqlite_extended=%d", e.Step, e.Role, e.Primary, e.Extended)
}

func (e *PersistenceError) Unwrap() error { return e.cause }

func runtimeAuthorityReadError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrFirewallAuthorityConflict
	}
	return err
}

func persistenceError(step, role string, err error) error {
	if err == nil || errors.Is(err, ErrFirewallAuthorityConflict) || errors.Is(err, ErrOperationConflict) || errors.Is(err, ErrRevisionConflict) || errors.Is(err, ErrRecordNotFound) {
		return err
	}
	var code sqlite3.Error
	e := &PersistenceError{Step: step, Role: role, cause: err}
	if errors.As(err, &code) {
		e.Primary, e.Extended = int(code.Code), int(code.ExtendedCode)
	}
	return e
}
