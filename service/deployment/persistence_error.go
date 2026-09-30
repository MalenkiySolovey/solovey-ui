package deployment

import (
	"errors"
	"fmt"

	sqlite3 "github.com/mattn/go-sqlite3"
	"gorm.io/gorm"
)

// PersistenceDiagnostic contains only owner-selected stages and storage codes.
// It must never include SQL, paths, row values, or the underlying error text.
type PersistenceDiagnostic struct {
	Stage              string `json:"stage"`
	Class              string `json:"class"`
	SQLiteCode         int    `json:"sqliteCode,omitempty"`
	SQLiteExtendedCode int    `json:"sqliteExtendedCode,omitempty"`
}

type persistenceStage string

const (
	postureSave  persistenceStage = "posture_save"
	stateRead    persistenceStage = "state_read"
	recoveryRead persistenceStage = "recovery_read"
	doctorSave   persistenceStage = "doctor_save"
)

type persistenceError struct {
	diagnostic PersistenceDiagnostic
	cause      error
}

func (e *persistenceError) Error() string {
	return fmt.Sprintf("deployment persistence: stage=%s class=%s sqlite=%d extended=%d", e.diagnostic.Stage, e.diagnostic.Class, e.diagnostic.SQLiteCode, e.diagnostic.SQLiteExtendedCode)
}
func (e *persistenceError) Unwrap() error        { return e.cause }
func (e *persistenceError) Is(target error) bool { return target == ErrStatePersistence }

// PersistenceFailure projects the closed diagnostic while errors.Is/As retain
// access to the original driver error internally.
func PersistenceFailure(err error) (PersistenceDiagnostic, bool) {
	var failure *persistenceError
	if !errors.As(err, &failure) {
		return PersistenceDiagnostic{}, false
	}
	return failure.diagnostic, true
}

func persistenceFailure(stage persistenceStage, err error) error {
	if err == nil || errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	diagnostic := PersistenceDiagnostic{Stage: string(stage), Class: "unknown"}
	var storage sqlite3.Error
	if errors.As(err, &storage) {
		diagnostic.SQLiteCode, diagnostic.SQLiteExtendedCode = int(storage.Code), int(storage.ExtendedCode)
		switch storage.Code {
		case sqlite3.ErrBusy:
			diagnostic.Class = "busy"
		case sqlite3.ErrLocked:
			diagnostic.Class = "locked"
		case sqlite3.ErrReadonly:
			diagnostic.Class = "readonly"
		case sqlite3.ErrConstraint:
			diagnostic.Class = "constraint"
		case sqlite3.ErrSchema:
			diagnostic.Class = "schema"
		case sqlite3.ErrIoErr, sqlite3.ErrFull, sqlite3.ErrCantOpen:
			diagnostic.Class = "io"
		}
	}
	return &persistenceError{diagnostic: diagnostic, cause: err}
}
