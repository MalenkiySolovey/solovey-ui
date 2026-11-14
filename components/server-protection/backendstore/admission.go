package backendstore

import (
	"context"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/durable"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	sqlite3 "github.com/mattn/go-sqlite3"
)

// AdmitMaintenance spans only protection journal/reconciliation work. The
// existing Backend owns the lease, generation and nested context admission.
func (s *Store) AdmitMaintenance(ctx context.Context) (context.Context, func(), error) {
	return dbsqlite.AcquireOperation(ctx)
}

type driverDiagnosticError struct{ cause error }

func (e *driverDiagnosticError) Error() string { return e.cause.Error() }
func (e *driverDiagnosticError) Unwrap() error { return e.cause }
func (e *driverDiagnosticError) PersistenceDiagnostic() durable.PersistenceDiagnostic {
	var driver sqlite3.Error
	if errors.As(e.cause, &driver) {
		return durable.PersistenceDiagnostic{Primary: int(driver.Code), Extended: int(driver.ExtendedCode)}
	}
	return durable.PersistenceDiagnostic{}
}

func backendError(err error) error {
	if err == nil {
		return nil
	}
	var diagnostic durable.PersistenceDiagnosticProvider
	if errors.As(err, &diagnostic) {
		return err
	}
	var driver sqlite3.Error
	if errors.As(err, &driver) {
		return &driverDiagnosticError{cause: err}
	}
	return err
}
