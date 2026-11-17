package backup

import canonical "github.com/MalenkiySolovey/solovey-backend/storage/backup"

const MaxRestoreBytes = canonical.MaxRestoreBytes

type BackupTableManifest = canonical.BackupTableManifest
type BackupOwnerManifest = canonical.BackupOwnerManifest
type BackupManifest = canonical.BackupManifest
type FileBackupManifest = canonical.FileBackupManifest
type RestoreOwnerStatus = canonical.RestoreOwnerStatus
type RestoreRehearsal = canonical.RestoreRehearsal
type RestoreExecutionResult = canonical.RestoreExecutionResult
type MigrationFinding = canonical.MigrationFinding
