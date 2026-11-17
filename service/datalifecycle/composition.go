// Package datalifecycle composes the canonical Backend lifecycle with Product owners.
package datalifecycle

import (
	"context"
	owner "github.com/MalenkiySolovey/solovey-ui/backend/datalifecycle"
	operationcoordination "github.com/MalenkiySolovey/solovey-ui/backend/operations/coordination"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	componentmigrations "github.com/MalenkiySolovey/solovey-ui/componenthost/migrations"
	componentregistry "github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	pressureService "github.com/MalenkiySolovey/solovey-ui/service/resourcepressure"
	"gorm.io/gorm"
	"io"
	"time"
)

type Manager = owner.Manager
type Resource = owner.Resource
type Preview = owner.Preview
type ExecuteRequest = owner.ExecuteRequest
type RestoreRequest = owner.RestoreRequest
type RecoveryBackup = owner.RecoveryBackup
type RecoveryBackupFile = owner.RecoveryBackupFile
type RetentionPolicyV1 = owner.RetentionPolicyV1
type PruneResult = owner.PruneResult
type OperationView = owner.OperationView

var (
	ErrPreviewChanged            = owner.ErrPreviewChanged
	ErrOperationConflict         = owner.ErrOperationConflict
	ErrBlocked                   = owner.ErrBlocked
	ErrRevisionMismatch          = owner.ErrRevisionMismatch
	ErrRecoveryRequired          = owner.ErrRecoveryRequired
	ErrRecoveryBackupUnavailable = owner.ErrRecoveryBackupUnavailable
	ErrRecoveryBackupExpired     = owner.ErrRecoveryBackupExpired
)

var shared = NewManager()

func Shared() *Manager { return shared }
func NewManager() *Manager {
	return owner.NewManager(owner.Dependencies{DB: dbsqlite.DB, DatabaseRoot: configstorage.GetDBFolderPath,
		Drop: coreservice.DropComponentData, Enabled: enabledstate.Enabled, Owners: componentOwners{}, BackupRestore: backupOwner{},
		Admit: func(class string) bool { return pressureService.Shared().Admission(class).Allowed }})
}
func HistoryRetentionPolicy() RetentionPolicyV1  { return owner.HistoryRetentionPolicy() }
func RestoreConfirmation(revision string) string { return owner.RestoreConfirmation(revision) }
func ReasonCode(err error) string                { return owner.ReasonCode(err) }
func ProjectChange(operation model.DataLifecycleOperation) operationcoordination.Change {
	return owner.ProjectChange(operation)
}
func ProjectJob(operation model.DataLifecycleOperation) operationcoordination.Job {
	return owner.ProjectJob(operation)
}
func ProjectOperation(operation model.DataLifecycleOperation) OperationView {
	return owner.ProjectOperation(operation)
}

type componentOwners struct{}

func (componentOwners) Installed() ([]string, error) {
	installed, err := installstate.InstalledComponents()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(installed))
	for _, item := range installed {
		ids = append(ids, item.ID)
	}
	return ids, nil
}
func (componentOwners) Lookup(id string) (owner.Owner, bool) {
	component, available := componentregistry.ComponentByID(id)
	if !available {
		return owner.Owner{}, false
	}
	_, canDrop := component.Lifecycle.(lifecycle.DataDropper)
	return owner.Owner{Manifest: component.Manifest, CanDrop: canDrop || component.Durable != nil,
		Inspect: func(ctx context.Context, db *gorm.DB, now time.Time) (string, []string) {
			return externalAuthority(ctx, component, db, now)
		}}, true
}
func (componentOwners) RemovalBlocker(ctx context.Context, db *gorm.DB, id string) string {
	return componentmigrations.RemovalBlocker(ctx, db, id)
}
func externalAuthority(ctx context.Context, component componentregistry.Component, db *gorm.DB, now time.Time) (string, []string) {
	if component.Durable != nil {
		status, err := component.Durable.Inspect(ctx, db, now)
		if err != nil {
			return "UNAVAILABLE", []string{"external_authority_unavailable"}
		}
		return status.State, append([]string(nil), status.ReasonCodes...)
	}
	inspector, ok := component.Lifecycle.(lifecycle.DropAuthorityInspector)
	if !ok {
		return "NOT_REQUIRED", []string{}
	}
	status := inspector.InspectDropAuthority(ctx, db, now)
	if status.State == "" {
		return "UNAVAILABLE", []string{"external_authority_contract_invalid"}
	}
	return status.State, append([]string(nil), status.ReasonCodes...)
}

type backupOwner struct{}

func (backupOwner) PrepareExport(ctx context.Context, exclude string) (string, func(), error) {
	return dbbackup.PrepareExportContext(ctx, exclude)
}
func (backupOwner) Rehearse(ctx context.Context, source io.ReadSeeker) (dbbackup.RestoreRehearsal, error) {
	return dbbackup.Rehearse(ctx, source)
}
func (backupOwner) RestoreWithRecoveryRoot(ctx context.Context, source io.ReadSeeker, root string) (dbbackup.RestoreExecutionResult, error) {
	return dbbackup.RestoreContextDetailedWithRecoveryRoot(ctx, source, root)
}
func (backupOwner) CompletePendingRestore(ctx context.Context) (bool, bool, error) {
	return dbbackup.CompletePendingRestore(ctx)
}
func (backupOwner) AbortPendingRestore() error { return dbbackup.AbortPendingRestore() }
