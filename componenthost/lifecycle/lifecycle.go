package lifecycle

import (
	"context"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"gorm.io/gorm"
)

type Context struct {
	Host componenthost.Deps
}

type Lifecycle interface {
	Start(context.Context, Context) error
	Stop(context.Context) error
}

type Migrator interface {
	Migrate(context.Context, Context) error
}

// StagedMigrator runs the owner's migration against the supplied restore
// candidate during rehearsal and rollback-protected acceptance. It must not
// consult or mutate a database other than the supplied handle.
type StagedMigrator interface {
	MigrateStaged(context.Context, *gorm.DB) error
}

// RestoreRehearser runs owner-specific restore normalization and postconditions
// on the supplied candidate, both in rehearsal and before restore acceptance.
type RestoreRehearser interface {
	RehearseRestore(context.Context, *gorm.DB) error
}

// BackupExclusions declares host-local tables whose schema is recreated by
// the owner migration. Exclusions apply even when the runtime is disabled.
type BackupExclusions interface {
	NonportableBackupTables() []string
}

type DataDropper interface {
	DropData(context.Context, Context) error
}

type DropAuthorityStatus struct {
	State       string
	ReasonCodes []string
}

// DropAuthorityInspector is a read-only owner-specific authority check used
// by Drop Data preview. Implementations must not persist reconciliation.
type DropAuthorityInspector interface {
	InspectDropAuthority(context.Context, *gorm.DB, time.Time) DropAuthorityStatus
}

type Noop struct{}

func (Noop) Start(context.Context, Context) error {
	return nil
}

func (Noop) Stop(context.Context) error {
	return nil
}
