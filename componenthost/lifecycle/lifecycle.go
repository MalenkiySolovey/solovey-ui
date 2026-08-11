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

// StagedMigrator runs the owner's migration against a disposable restore copy.
// It must not consult or mutate the process-global database.
type StagedMigrator interface {
	MigrateStaged(context.Context, *gorm.DB) error
}

// RestoreRehearser runs owner-specific restore normalization and postconditions
// against a disposable copy before the live database can be quiesced.
type RestoreRehearser interface {
	RehearseRestore(context.Context, *gorm.DB) error
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
