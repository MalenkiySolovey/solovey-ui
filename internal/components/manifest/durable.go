package manifest

import canonical "github.com/MalenkiySolovey/solovey-host/componenthost/manifest"

const (
	DurableResourceSchemaV1 = canonical.DurableResourceSchemaV1
	IndexPolicyOwnedTables  = canonical.IndexPolicyOwnedTables
	BackupCodecSQLiteOwner  = canonical.BackupCodecSQLiteOwner
	RestoreHookManifestV1   = canonical.RestoreHookManifestV1
	DropPlanOwnerLifecycle  = canonical.DropPlanOwnerLifecycle
	DropPostconditionV1     = canonical.DropPostconditionV1
	RedactionSensitive      = canonical.RedactionSensitive
	RedactionConfidential   = canonical.RedactionConfidential
	PortabilityPortable     = canonical.PortabilityPortable
	PortabilityHostBound    = canonical.PortabilityHostBound
	FileBackupExcluded      = canonical.FileBackupExcluded
	FileBackupOpaque        = canonical.FileBackupOpaque
)

type DurableResourceManifestV1 = canonical.DurableResourceManifestV1
type Database = canonical.Database
type DurableFileResource = canonical.DurableFileResource
type CompatibilityRange = canonical.CompatibilityRange
