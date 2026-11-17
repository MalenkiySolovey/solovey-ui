// Package backup owns durable backup/restore projections and the candidate database context.
package backup

import (
	"context"
	componentmanifest "github.com/MalenkiySolovey/solovey-host/componenthost/manifest"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
)

const MaxRestoreBytes = int64(512 << 20)

// MigrationFinding projects fixed structural diagnostics supplied by a migration owner.
type MigrationFinding struct {
	Kind                   string `json:"kind"`
	Path                   string `json:"path"`
	Code                   string `json:"code"`
	Severity               string `json:"severity"`
	Message                string `json:"message"`
	MigrationOutcome       string `json:"migrationOutcome,omitempty"`
	AutomaticAvailable     bool   `json:"automaticAvailable,omitempty"`
	OperatorActionRequired bool   `json:"operatorActionRequired,omitempty"`
}

type BackupTableManifest struct {
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	Rows          int64  `json:"rows"`
	SchemaDigest  string `json:"schemaDigest"`
	ContentDigest string `json:"contentDigest"`
	Excluded      bool   `json:"excluded"`
	ExclusionCode string `json:"exclusionCode,omitempty"`
}

type BackupOwnerManifest struct {
	ID               string                                       `json:"id"`
	Installed        bool                                         `json:"installed"`
	Available        bool                                         `json:"available"`
	Mode             string                                       `json:"mode"`
	ResourceManifest *componentmanifest.DurableResourceManifestV1 `json:"resourceManifest,omitempty"`
}

type BackupManifest struct {
	Schema             string                `json:"schema"`
	BackupID           string                `json:"backupId"`
	CreatedAt          int64                 `json:"createdAt"`
	AppVersion         string                `json:"appVersion"`
	CoreSchema         string                `json:"coreSchema"`
	SQLiteModule       string                `json:"sqliteModule"`
	SQLiteRuntime      string                `json:"sqliteRuntime"`
	SQLiteSourceID     string                `json:"sqliteSourceId"`
	ReleaseSequence    uint64                `json:"releaseSequence"`
	ReleaseVersion     string                `json:"releaseVersion"`
	DeploymentProfile  string                `json:"deploymentProfile"`
	DeploymentRevision string                `json:"deploymentRevision"`
	Encryption         string                `json:"encryption"`
	MaxBytes           int64                 `json:"maxBytes"`
	Owners             []BackupOwnerManifest `json:"owners"`
	Tables             []BackupTableManifest `json:"tables"`
	Compatibility      string                `json:"compatibility"`
	Files              *FileBackupManifest   `json:"files,omitempty"`
}

type FileBackupManifest struct {
	Schema        string   `json:"schema"`
	Owners        []string `json:"owners"`
	Rows          int64    `json:"rows"`
	SchemaDigest  string   `json:"schemaDigest"`
	ContentDigest string   `json:"contentDigest"`
}

type RestoreOwnerStatus struct {
	ID               string `json:"id"`
	Installed        bool   `json:"installed"`
	Available        bool   `json:"available"`
	Included         bool   `json:"included"`
	Mode             string `json:"mode"`
	SchemaVersion    string `json:"schemaVersion,omitempty"`
	ManifestChecksum string `json:"manifestChecksum,omitempty"`
	Compatibility    string `json:"compatibility"`
	HookStatus       string `json:"hookStatus"`
}

type RestoreRehearsal struct {
	Schema               string               `json:"schema"`
	State                string               `json:"state"`
	Possible             bool                 `json:"possible"`
	BackupDigest         string               `json:"backupDigest"`
	BackupBytes          int64                `json:"backupBytes"`
	ManifestStatus       string               `json:"manifestStatus"`
	Manifest             *BackupManifest      `json:"manifest,omitempty"`
	Integrity            string               `json:"integrity"`
	SchemaCompatibility  string               `json:"schemaCompatibility"`
	MigrationPlan        string               `json:"migrationPlan"`
	ReleaseCompatibility string               `json:"releaseCompatibility"`
	SpaceStatus          string               `json:"spaceStatus"`
	Owners               []RestoreOwnerStatus `json:"owners"`
	ReasonCodes          []string             `json:"reasonCodes"`
	MigrationFindings    []MigrationFinding   `json:"migrationFindings,omitempty"`
	Revision             string               `json:"revision"`
	GeneratedAt          int64                `json:"generatedAt"`
}

type RestoreExecutionResult struct {
	Rehearsal              RestoreRehearsal `json:"rehearsal"`
	RecoveryBackupRef      string           `json:"recoveryBackupRef"`
	RecoveryCleanupPending bool             `json:"recoveryCleanupPending"`
	RestartPending         bool             `json:"restartPending"`
	maintenance            *dbsqlite.Maintenance
}

// DatabaseContext lets the restore caller persist its operation authority
// against the private candidate. It is temporary and is never a cached DB.
func (r RestoreExecutionResult) DatabaseContext(ctx context.Context) context.Context {
	if r.maintenance != nil && r.maintenance.Active() {
		return r.maintenance.Context(ctx)
	}
	return ctx
}

// WithMaintenance retains the existing temporary SQLite candidate authority.
func (r RestoreExecutionResult) WithMaintenance(owner *dbsqlite.Maintenance) RestoreExecutionResult {
	r.maintenance = owner
	return r
}
