// Package durable defines owner-scoped component data operations. Backend
// binds each capability to one storage generation and operation. These ports
// cannot open a database, promote a candidate, or authorize component removal.
package durable

import (
	"context"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-host/hostref"
)

type Storage string

const (
	Live      Storage = "LIVE"
	Candidate Storage = "CANDIDATE"
)

type Scope struct {
	Host             hostref.Ref
	OwnerID          string
	Storage          Storage
	SchemaVersion    string
	MigrationVersion string
}

func (s Scope) Validate() error {
	if !s.Host.IsLocal() || s.OwnerID == "" || s.SchemaVersion == "" || s.MigrationVersion == "" || (s.Storage != Live && s.Storage != Candidate) {
		return errors.New("durable operation scope is unavailable")
	}
	return nil
}

// Outcome reports evidence, never authority. UNKNOWN retains the existing
// owner's journal/recovery obligation; it must not be reported as completion.
type Outcome string

const (
	Verified Outcome = "VERIFIED"
	Rejected Outcome = "REJECTED"
	Unknown  Outcome = "UNKNOWN"
)

type Result struct {
	Scope   Scope
	Outcome Outcome
}

type Failure struct {
	Outcome Outcome
	Cause   error
}

func (e *Failure) Error() string {
	return "durable operation " + string(e.Outcome) + ": " + e.Cause.Error()
}
func (e *Failure) Unwrap() error { return e.Cause }

type AuthorityStatus struct {
	State       string
	ReasonCodes []string
}

// Each operation receives only the capability admitted by its Backend owner.
// Callbacks never receive transaction handles, paths or general CRUD methods.
type Migration interface {
	Scope() Scope
	MigrateAndValidate(context.Context) (Result, error)
}

type Restore interface {
	Scope() Scope
	NormalizeRestore(context.Context) (Result, error)
}

type Inspection interface {
	Scope() Scope
	InspectDropAuthority(context.Context, time.Time) (AuthorityStatus, error)
}

type DataDrop interface {
	Scope() Scope
	DropOwnedData(context.Context) (Result, error)
}

// Lifecycle supplies feature-specific sequencing without acquiring Backend
// storage authority. Backend verifies that the admitted capability actually ran.
type Lifecycle interface {
	Migrate(context.Context, Migration) error
	RehearseRestore(context.Context, Restore) error
	InspectDropAuthority(context.Context, Inspection, time.Time) (AuthorityStatus, error)
	DropData(context.Context, DataDrop) error
}
