package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

const (
	DurableResourceSchemaV1 = "solovey.durable-resource/v1"
	IndexPolicyOwnedTables  = "ALL_OWNED_TABLE_INDEXES"
	BackupCodecSQLiteOwner  = "SQLITE_OWNER_V1"
	RestoreHookManifestV1   = "MIGRATE_AND_VALIDATE_V1"
	DropPlanOwnerLifecycle  = "OWNER_LIFECYCLE_V1"
	DropPostconditionV1     = "DECLARED_RESOURCES_ABSENT_V1"
	RedactionSensitive      = "SENSITIVE"
	RedactionConfidential   = "CONFIDENTIAL"
	PortabilityPortable     = "PORTABLE"
	PortabilityHostBound    = "HOST_BOUND"
	FileBackupExcluded      = "EXCLUDED_HOST_LOCAL"
	FileBackupOpaque        = "OPAQUE_PORTABLE"
)

// DurableResourceManifestV1 is the installed-owner data contract. Discovery
// does not depend on whether the owner is currently enabled or running.
type DurableResourceManifestV1 struct {
	Schema             string                `json:"schema,omitempty"`
	SchemaVersion      string                `json:"schemaVersion,omitempty"`
	SchemaChecksum     string                `json:"schemaChecksum,omitempty"`
	MigrationVersion   string                `json:"migrationVersion,omitempty"`
	MigrationChecksum  string                `json:"migrationChecksum,omitempty"`
	Tables             []string              `json:"tables,omitempty"`
	IndexPolicy        string                `json:"indexPolicy,omitempty"`
	Settings           []string              `json:"settings,omitempty"`
	Secrets            []string              `json:"secrets,omitempty"`
	Files              []DurableFileResource `json:"files,omitempty"`
	BackupCodec        string                `json:"backupCodec,omitempty"`
	RestoreHook        string                `json:"restoreHook,omitempty"`
	DropDataPlan       string                `json:"dropDataPlan,omitempty"`
	DropPostcondition  string                `json:"dropPostcondition,omitempty"`
	RedactionClass     string                `json:"redactionClass,omitempty"`
	PortabilityClass   string                `json:"portabilityClass,omitempty"`
	CompatibilityRange CompatibilityRange    `json:"compatibilityRange,omitempty"`
}

// Database is the source-compatible name used by older component code. Its
// value is now the complete v1 durable-resource manifest.
type Database = DurableResourceManifestV1

type DurableFileResource struct {
	Path        string `json:"path"`
	BackupClass string `json:"backupClass"`
	Redaction   string `json:"redaction"`
	Portability string `json:"portability"`
}

type CompatibilityRange struct {
	MinimumSchema string `json:"minimumSchema,omitempty"`
	MaximumSchema string `json:"maximumSchema,omitempty"`
}

func (d DurableResourceManifestV1) Declared() bool {
	return len(d.Tables)+len(d.Settings)+len(d.Secrets)+len(d.Files) > 0 || d.Schema != ""
}

func (d DurableResourceManifestV1) Normalized(ownerVersion string) DurableResourceManifestV1 {
	if !d.Declared() {
		return DurableResourceManifestV1{}
	}
	if ownerVersion == "" {
		ownerVersion = "1"
	}
	defaults := map[*string]string{
		&d.Schema: DurableResourceSchemaV1, &d.SchemaVersion: ownerVersion,
		&d.MigrationVersion: ownerVersion, &d.IndexPolicy: IndexPolicyOwnedTables,
		&d.BackupCodec: BackupCodecSQLiteOwner, &d.RestoreHook: RestoreHookManifestV1,
		&d.DropDataPlan: DropPlanOwnerLifecycle, &d.DropPostcondition: DropPostconditionV1,
		&d.RedactionClass: RedactionSensitive, &d.PortabilityClass: PortabilityPortable,
	}
	for target, value := range defaults {
		if *target == "" {
			*target = value
		}
	}
	if d.CompatibilityRange.MinimumSchema == "" {
		d.CompatibilityRange.MinimumSchema = d.SchemaVersion
	}
	if d.CompatibilityRange.MaximumSchema == "" {
		d.CompatibilityRange.MaximumSchema = d.SchemaVersion
	}
	d.Tables, d.Settings, d.Secrets = sortedUnique(d.Tables), sortedUnique(d.Settings), sortedUnique(d.Secrets)
	sort.Slice(d.Files, func(i, j int) bool { return d.Files[i].Path < d.Files[j].Path })
	d.SchemaChecksum = durableChecksum(d, false)
	d.MigrationChecksum = durableChecksum(d, true)
	return d
}

func (d DurableResourceManifestV1) ContractChecksum() string {
	normalized := d.Normalized(d.SchemaVersion)
	return durableChecksum(normalized, false)
}

func durableChecksum(d DurableResourceManifestV1, migration bool) string {
	d.SchemaChecksum, d.MigrationChecksum = "", ""
	if migration {
		d.Schema += "/migration"
	}
	data, _ := json.Marshal(d)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (d DurableResourceManifestV1) Validate(ownerID, ownerVersion string) error {
	if !d.Declared() {
		return nil
	}
	// Validate declarations before normalization. Normalization is allowed to
	// sort canonical data, but must never hide duplicate ownership claims.
	if err := validateUniqueIdentifiers(ownerID, "table", d.Tables, databaseTablePattern); err != nil {
		return err
	}
	if err := validateUniqueIdentifiers(ownerID, "setting", d.Settings, durableKeyPattern); err != nil {
		return err
	}
	if err := validateUniqueIdentifiers(ownerID, "secret", d.Secrets, durableKeyPattern); err != nil {
		return err
	}
	seenDeclaredFiles := make(map[string]struct{}, len(d.Files))
	for _, file := range d.Files {
		clean := path.Clean(strings.ReplaceAll(file.Path, "\\", "/"))
		if _, duplicate := seenDeclaredFiles[clean]; duplicate {
			return fmt.Errorf("component %q durable file path %q is duplicated", ownerID, file.Path)
		}
		seenDeclaredFiles[clean] = struct{}{}
	}
	normalized := d.Normalized(ownerVersion)
	if normalized.Schema != DurableResourceSchemaV1 || normalized.SchemaVersion == "" || normalized.MigrationVersion == "" ||
		normalized.IndexPolicy != IndexPolicyOwnedTables || normalized.BackupCodec != BackupCodecSQLiteOwner ||
		normalized.RestoreHook != RestoreHookManifestV1 || normalized.DropDataPlan != DropPlanOwnerLifecycle ||
		normalized.DropPostcondition != DropPostconditionV1 {
		return fmt.Errorf("component %q durable-resource contract is unsupported", ownerID)
	}
	if d.SchemaChecksum != "" && d.SchemaChecksum != normalized.SchemaChecksum {
		return fmt.Errorf("component %q durable schema checksum does not match its resources", ownerID)
	}
	if d.MigrationChecksum != "" && d.MigrationChecksum != normalized.MigrationChecksum {
		return fmt.Errorf("component %q durable migration checksum does not match its resources", ownerID)
	}
	if normalized.RedactionClass != RedactionSensitive && normalized.RedactionClass != RedactionConfidential {
		return fmt.Errorf("component %q durable redaction class is unsupported", ownerID)
	}
	if normalized.PortabilityClass != PortabilityPortable && normalized.PortabilityClass != PortabilityHostBound {
		return fmt.Errorf("component %q durable portability class is unsupported", ownerID)
	}
	if normalized.CompatibilityRange.MinimumSchema == "" || normalized.CompatibilityRange.MaximumSchema == "" {
		return fmt.Errorf("component %q durable compatibility range is incomplete", ownerID)
	}
	if err := validateUniqueIdentifiers(ownerID, "table", normalized.Tables, databaseTablePattern); err != nil {
		return err
	}
	if len(normalized.Tables) > 128 || len(normalized.Settings)+len(normalized.Secrets) > 256 || len(normalized.Files) > 32 {
		return fmt.Errorf("component %q durable-resource inventory is too large", ownerID)
	}
	if err := validateUniqueIdentifiers(ownerID, "setting", normalized.Settings, durableKeyPattern); err != nil {
		return err
	}
	if err := validateUniqueIdentifiers(ownerID, "secret", normalized.Secrets, durableKeyPattern); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, key := range normalized.Settings {
		seen[key] = struct{}{}
	}
	for _, key := range normalized.Secrets {
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("component %q durable key %q is both a setting and secret", ownerID, key)
		}
	}
	seenFiles := map[string]struct{}{}
	for _, file := range normalized.Files {
		clean := path.Clean(strings.ReplaceAll(file.Path, "\\", "/"))
		if clean == "." || clean != file.Path || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "../") || strings.Contains(clean, "//") {
			return fmt.Errorf("component %q durable file path %q is unsafe", ownerID, file.Path)
		}
		if _, duplicate := seenFiles[clean]; duplicate {
			return fmt.Errorf("component %q durable file path %q is duplicated", ownerID, file.Path)
		}
		seenFiles[clean] = struct{}{}
		if file.BackupClass != FileBackupExcluded && file.BackupClass != FileBackupOpaque {
			return fmt.Errorf("component %q durable file %q has unsupported backup class", ownerID, file.Path)
		}
		if file.Redaction != RedactionSensitive && file.Redaction != RedactionConfidential ||
			file.Portability != PortabilityPortable && file.Portability != PortabilityHostBound {
			return fmt.Errorf("component %q durable file %q has unsupported classification", ownerID, file.Path)
		}
	}
	return nil
}

func validateUniqueIdentifiers(ownerID, kind string, values []string, pattern interface{ MatchString(string) bool }) error {
	seen := map[string]struct{}{}
	for _, value := range values {
		if !pattern.MatchString(value) {
			return fmt.Errorf("component %q durable %s %q is invalid", ownerID, kind, value)
		}
		if _, duplicate := seen[value]; duplicate {
			return fmt.Errorf("component %q durable %s %q is duplicated", ownerID, kind, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func sortedUnique(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	if len(result) < 2 {
		return result
	}
	out := result[:0]
	for _, value := range result {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}
