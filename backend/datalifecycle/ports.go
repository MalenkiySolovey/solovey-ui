package datalifecycle

import (
	"context"
	"errors"
	componentmanifest "github.com/MalenkiySolovey/solovey-host/componenthost/manifest"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/backend/storage/backup"
	"gorm.io/gorm"
	"io"
	"time"
)

// Owner projects the ComponentHost capabilities needed by data lifecycle admission.
type Owner struct {
	Manifest componentmanifest.Manifest
	CanDrop  bool
	Inspect  func(context.Context, *gorm.DB, time.Time) (string, []string)
}

// OwnerCatalog observes the existing installed and runtime ComponentHost owners.
type OwnerCatalog interface {
	Installed() ([]string, error)
	Lookup(string) (Owner, bool)
	RemovalBlocker(context.Context, *gorm.DB, string) string
}

// BackupRestore delegates candidate creation and acceptance to the existing backup owner.
type BackupRestore interface {
	PrepareExport(context.Context, string) (string, func(), error)
	Rehearse(context.Context, io.ReadSeeker) (dbbackup.RestoreRehearsal, error)
	RestoreWithRecoveryRoot(context.Context, io.ReadSeeker, string) (dbbackup.RestoreExecutionResult, error)
	CompletePendingRestore(context.Context) (bool, bool, error)
	AbortPendingRestore() error
}
type Dependencies struct {
	DB            func() *gorm.DB
	DatabaseRoot  func() string
	Owners        OwnerCatalog
	BackupRestore BackupRestore
	Drop          func(context.Context, string) error
	Enabled       func(componentmanifest.Manifest) (bool, error)
	Admit         func(string) bool
}

func (m *Manager) installedOwners() ([]string, error) {
	if m == nil || m.Owners == nil {
		return nil, errors.New("installed owner catalog unavailable")
	}
	return m.Owners.Installed()
}
func (m *Manager) databaseRoot() string {
	if m == nil || m.DatabaseRoot == nil {
		return ""
	}
	return m.DatabaseRoot()
}
