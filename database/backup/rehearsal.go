package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	"github.com/MalenkiySolovey/solovey-ui/database/migration"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	componentmanifest "github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/durableowner"
	"github.com/shirou/gopsutil/v4/disk"
	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const MaxRestoreBytes = int64(512 << 20)

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
	Revision             string               `json:"revision"`
	GeneratedAt          int64                `json:"generatedAt"`
}

func Rehearse(ctx context.Context, source io.ReadSeeker) (RestoreRehearsal, error) {
	result := RestoreRehearsal{Schema: "solovey.restore-rehearsal/v1", State: "REJECTED", ManifestStatus: "UNVERIFIED",
		Integrity: "NOT_CHECKED", SchemaCompatibility: "UNKNOWN", MigrationPlan: "UNKNOWN",
		ReleaseCompatibility: "UNKNOWN", SpaceStatus: "UNKNOWN", Owners: []RestoreOwnerStatus{}, ReasonCodes: []string{},
		GeneratedAt: time.Now().UTC().Unix()}
	if ctx == nil || source == nil {
		return result, errors.New("restore rehearsal input is unavailable")
	}
	staged, cleanup, digest, size, err := stageRehearsalFile(ctx, source)
	if err != nil {
		result.ReasonCodes = []string{"backup_staging_failed"}
		result.Revision = restoreRehearsalRevision(result)
		return result, err
	}
	defer cleanup()
	result.BackupDigest, result.BackupBytes = digest, size
	if err := validateSQLiteBackup(staged); err != nil {
		var versionErr *dbsqlite.UnsupportedVersionError
		if !errors.As(err, &versionErr) {
			result.Integrity, result.ReasonCodes = "FAILED", []string{"backup_integrity_or_version_invalid"}
			result.Revision = restoreRehearsalRevision(result)
			return result, nil
		}
		result.Integrity = "VERIFIED"
		if versionErr.Label == "core schema" {
			result.SchemaCompatibility = "FUTURE_UNSUPPORTED"
			result.ReasonCodes = append(result.ReasonCodes, "future_core_schema")
		} else {
			result.ReleaseCompatibility = "FUTURE_UNSUPPORTED"
			result.ReasonCodes = append(result.ReasonCodes, "future_release")
		}
	}
	if result.Integrity == "NOT_CHECKED" {
		result.Integrity = "VERIFIED"
	}
	probe, err := gorm.Open(gormsqlite.Open(sqliteReadOnlyDSN(staged)), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		result.ReasonCodes = append(result.ReasonCodes, "backup_probe_unavailable")
		result.Revision = restoreRehearsalRevision(result)
		return result, nil
	}
	if sqlDB, dbErr := probe.DB(); dbErr == nil {
		defer sqlDB.Close()
	}
	manifest, manifestStatus, manifestErr := loadManifestForRehearsal(ctx, probe)
	if manifestErr != nil {
		result.ManifestStatus = "ABSENT_OR_INVALID"
		result.ReasonCodes = append(result.ReasonCodes, "backup_manifest_invalid")
	} else {
		result.ManifestStatus, result.Manifest = manifestStatus, &manifest
		if result.SchemaCompatibility != "FUTURE_UNSUPPORTED" {
			result.SchemaCompatibility, result.MigrationPlan = schemaCompatibility(manifest.CoreSchema)
		}
		if result.ReleaseCompatibility != "FUTURE_UNSUPPORTED" {
			result.ReleaseCompatibility = releaseCompatibility(manifest.AppVersion)
		}
		if result.SchemaCompatibility == "FUTURE_UNSUPPORTED" {
			result.ReasonCodes = append(result.ReasonCodes, "future_core_schema")
		}
		if result.ReleaseCompatibility == "FUTURE_UNSUPPORTED" {
			result.ReasonCodes = append(result.ReasonCodes, "future_release")
		}
		result.Owners = restoreOwnerCompatibility(manifest)
		for _, owner := range result.Owners {
			if owner.Included && !owner.Available {
				result.ReasonCodes = append(result.ReasonCodes, "backup_owner_unavailable:"+owner.ID)
			}
			if owner.Installed && !owner.Included {
				result.ReasonCodes = append(result.ReasonCodes, "installed_owner_missing_from_backup:"+owner.ID)
			}
			if owner.Compatibility == "INCOMPATIBLE" {
				result.ReasonCodes = append(result.ReasonCodes, "backup_owner_incompatible:"+owner.ID)
			}
		}
		if len(result.ReasonCodes) == 0 {
			statuses, migrationErr := rehearseMigrationsAndOwners(ctx, staged, result.Owners)
			result.Owners = statuses
			if migrationErr != nil {
				result.MigrationPlan = "FAILED"
				result.ReasonCodes = append(result.ReasonCodes, "staged_migration_or_owner_hook_failed")
			} else if result.MigrationPlan == "REQUIRED" {
				result.MigrationPlan = "REHEARSED"
			} else {
				result.MigrationPlan = "NOT_REQUIRED_REHEARSED"
			}
		}
	}
	if usage, usageErr := disk.Usage(configstorage.GetDBFolderPath()); usageErr == nil && usage.Free >= uint64(size*3) {
		result.SpaceStatus = "SUFFICIENT"
	} else {
		result.SpaceStatus = "INSUFFICIENT_OR_UNAVAILABLE"
		result.ReasonCodes = append(result.ReasonCodes, "restore_space_not_proven")
	}
	result.ReasonCodes = uniqueStrings(result.ReasonCodes)
	result.Possible = (result.ManifestStatus == "VERIFIED" || result.ManifestStatus == "LEGACY_EXPLICITLY_ADAPTED") && result.Integrity == "VERIFIED" &&
		result.SchemaCompatibility != "FUTURE_UNSUPPORTED" && result.ReleaseCompatibility != "FUTURE_UNSUPPORTED" &&
		result.SpaceStatus == "SUFFICIENT" && len(result.ReasonCodes) == 0
	if result.Possible {
		result.State = "READY"
	}
	result.Revision = restoreRehearsalRevision(result)
	return result, nil
}

func loadManifestForRehearsal(ctx context.Context, db *gorm.DB) (BackupManifest, string, error) {
	manifest, err := LoadAndVerifyManifest(ctx, db)
	if err == nil {
		return manifest, "VERIFIED", nil
	}
	legacy, legacyErr := synthesizeSupportedLegacyManifest(ctx, db)
	if legacyErr != nil {
		return BackupManifest{}, "", errors.Join(err, legacyErr)
	}
	return legacy, "LEGACY_EXPLICITLY_ADAPTED", nil
}

func synthesizeSupportedLegacyManifest(ctx context.Context, db *gorm.DB) (BackupManifest, error) {
	if ctx == nil || db == nil || db.Migrator().HasTable(BackupManifestTable) || !db.Migrator().HasTable("settings") {
		return BackupManifest{}, errors.New("legacy backup shape is not supported")
	}
	var appVersion, coreSchema string
	if err := db.WithContext(ctx).Raw("SELECT value FROM settings WHERE key = ? LIMIT 1", "version").Scan(&appVersion).Error; err != nil {
		return BackupManifest{}, err
	}
	if err := db.WithContext(ctx).Raw("SELECT value FROM settings WHERE key = ? LIMIT 1", "coreSchemaVersion").Scan(&coreSchema).Error; err != nil {
		return BackupManifest{}, err
	}
	minimumComparison, minimumOK := versionpolicy.CompareVersions(appVersion, "1.4.1")
	currentComparison, currentOK := versionpolicy.CompareVersions(appVersion, configidentity.GetVersion())
	if !minimumOK || !currentOK || minimumComparison < 0 || currentComparison > 0 {
		return BackupManifest{}, errors.New("legacy backup version is outside the supported range")
	}
	if coreSchema == "" {
		coreSchema = "1.7"
	}
	coreComparison, coreOK := versionpolicy.CompareVersions(coreSchema, "1.11")
	if !coreOK || coreComparison > 0 {
		return BackupManifest{}, errors.New("legacy core schema is unsupported")
	}
	known := map[string]bool{}
	for _, table := range backupTables() {
		known[table.name] = !table.alwaysExclude
	}
	var names []string
	if err := db.WithContext(ctx).Raw("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name").Scan(&names).Error; err != nil {
		return BackupManifest{}, err
	}
	if len(names) == 0 || len(names) > MaxBackupTables {
		return BackupManifest{}, errors.New("legacy backup table inventory is invalid")
	}
	if err := validateSQLiteObjectInventory(ctx, db, known, false); err != nil {
		return BackupManifest{}, fmt.Errorf("legacy backup schema inventory is invalid: %w", err)
	}
	var foreignKeyViolations int64
	if err := db.WithContext(ctx).Raw("SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&foreignKeyViolations).Error; err != nil {
		return BackupManifest{}, fmt.Errorf("legacy backup foreign-key integrity is unavailable: %w", err)
	}
	if foreignKeyViolations != 0 {
		return BackupManifest{}, errors.New("legacy backup foreign-key integrity failed")
	}
	manifest := BackupManifest{Schema: BackupManifestSchema, AppVersion: appVersion, CoreSchema: coreSchema,
		SQLiteModule: dbsqlite.SQLiteModuleVersion, Encryption: "LEGACY_INNER_PLAINTEXT", MaxBytes: MaxRestoreBytes,
		Owners:        []BackupOwnerManifest{{ID: "core", Installed: true, Available: true, Mode: "LEGACY_TYPED"}},
		Compatibility: "LEGACY_EXPLICIT_FORWARD_MIGRATABLE"}
	if runtimeStatus, runtimeErr := dbsqlite.InspectRuntime(db); runtimeErr == nil {
		manifest.SQLiteRuntime, manifest.SQLiteSourceID = runtimeStatus.RuntimeVersion, runtimeStatus.SourceID
	}
	for _, name := range names {
		if !known[name] {
			return BackupManifest{}, errors.New("legacy backup contains an undeclared durable table")
		}
		entry, err := digestBackupTable(ctx, db, "core", name)
		if err != nil {
			return BackupManifest{}, err
		}
		manifest.Tables = append(manifest.Tables, entry)
	}
	manifest.BackupID = backupManifestDigest(manifest)
	return manifest, nil
}

func stageRehearsalFile(ctx context.Context, source io.ReadSeeker) (string, func(), string, int64, error) {
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return "", nil, "", 0, err
	}
	directory := filepath.Join(configstorage.GetDBFolderPath(), "restore-rehearsal")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", nil, "", 0, err
	}
	file, err := os.CreateTemp(directory, "rehearsal-*.db")
	if err != nil {
		return "", nil, "", 0, err
	}
	path := file.Name()
	cleanup := func() { cleanupRestoreFile(path) }
	hash := sha256.New()
	written, copyErr := copyContext(ctx, io.MultiWriter(file, hash), io.LimitReader(source, MaxRestoreBytes+1))
	syncErr, closeErr := file.Sync(), file.Close()
	_, seekErr := source.Seek(0, io.SeekStart)
	if copyErr != nil || syncErr != nil || closeErr != nil || seekErr != nil || written <= 0 || written > MaxRestoreBytes {
		cleanup()
		return "", nil, "", written, errors.Join(copyErr, syncErr, closeErr, seekErr, errors.New("restore rehearsal file exceeds bounds"))
	}
	return path, cleanup, hex.EncodeToString(hash.Sum(nil)), written, nil
}

func copyContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 128<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			written, writeErr := destination.Write(buffer[:count])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != count {
				return total, io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func schemaCompatibility(value string) (string, string) {
	comparison, ok := versionpolicy.CompareVersions(value, "1.11")
	if !ok || comparison > 0 {
		return "FUTURE_UNSUPPORTED", "BLOCKED"
	}
	if comparison < 0 {
		return "FORWARD_MIGRATABLE", "REQUIRED"
	}
	return "EXACT", "NOT_REQUIRED"
}

func releaseCompatibility(value string) string {
	comparison, ok := versionpolicy.CompareVersions(value, configidentity.GetVersion())
	if !ok || comparison > 0 {
		return "FUTURE_UNSUPPORTED"
	}
	if comparison < 0 {
		return "OLDER_COMPATIBLE"
	}
	return "EXACT"
}

func restoreOwnerCompatibility(manifest BackupManifest) []RestoreOwnerStatus {
	byID := map[string]RestoreOwnerStatus{}
	for _, owner := range manifest.Owners {
		current, available := durableowner.Lookup(owner.ID)
		if owner.ID == "core" {
			available = true
		}
		status := RestoreOwnerStatus{ID: owner.ID, Included: true, Available: available, Mode: owner.Mode,
			Compatibility: "UNKNOWN", HookStatus: "NOT_RUN"}
		if owner.ResourceManifest != nil {
			status.SchemaVersion, status.ManifestChecksum = owner.ResourceManifest.SchemaVersion, owner.ResourceManifest.SchemaChecksum
		}
		if owner.ID == "core" {
			status.Compatibility = "COMPATIBLE"
		} else if available && ownerResourcesCompatible(owner.ResourceManifest, current.Database) {
			status.Compatibility = "COMPATIBLE"
		} else if available {
			status.Compatibility = "INCOMPATIBLE"
		}
		byID[owner.ID] = status
	}
	if installed, err := installstate.InstalledComponents(); err == nil {
		for _, owner := range installed {
			status := byID[owner.ID]
			status.ID, status.Installed = owner.ID, true
			if _, available := durableowner.Lookup(owner.ID); available {
				status.Available = true
			}
			if !status.Included {
				status.Compatibility, status.HookStatus = "MISSING", "NOT_RUN"
			}
			byID[owner.ID] = status
		}
	}
	result := make([]RestoreOwnerStatus, 0, len(byID))
	for _, owner := range byID {
		result = append(result, owner)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func ownerResourcesCompatible(backup *componentmanifest.DurableResourceManifestV1, current componentmanifest.DurableResourceManifestV1) bool {
	current = current.Normalized(current.SchemaVersion)
	if backup == nil {
		return !current.Declared()
	}
	backupNormalized := backup.Normalized(backup.SchemaVersion)
	if !backupNormalized.Declared() || !current.Declared() {
		return backupNormalized.Declared() == current.Declared()
	}
	if backupNormalized.SchemaChecksum == current.SchemaChecksum {
		return true
	}
	lower, lowerOK := versionpolicy.CompareVersions(schemaSemver(backupNormalized.SchemaVersion), schemaSemver(current.CompatibilityRange.MinimumSchema))
	upper, upperOK := versionpolicy.CompareVersions(schemaSemver(backupNormalized.SchemaVersion), schemaSemver(current.CompatibilityRange.MaximumSchema))
	return lowerOK && upperOK && lower >= 0 && upper <= 0 && backupNormalized.SchemaVersion != current.SchemaVersion
}

func schemaSemver(value string) string {
	if len(strings.Split(value, ".")) == 1 {
		return value + ".0"
	}
	return value
}

func rehearseMigrationsAndOwners(ctx context.Context, staged string, statuses []RestoreOwnerStatus) ([]RestoreOwnerStatus, error) {
	copyPath := staged + ".migration"
	cleanup := func() { cleanupRestoreFile(copyPath) }
	defer cleanup()
	if err := cloneRestoreFile(ctx, staged, copyPath); err != nil {
		return statuses, err
	}
	if err := migration.MigratePath(copyPath, migration.Options{}); err != nil {
		return statuses, err
	}
	db, err := gorm.Open(gormsqlite.Open(copyPath+"?_busy_timeout=10000&_foreign_keys=on"), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		return statuses, err
	}
	if sqlDB, sqlErr := db.DB(); sqlErr == nil {
		defer sqlDB.Close()
	}
	for index := range statuses {
		status := &statuses[index]
		if status.ID == "core" {
			status.HookStatus = "PASSED"
			continue
		}
		if !status.Included || !status.Available || status.Compatibility != "COMPATIBLE" {
			continue
		}
		if err := durableowner.RunStagedRestore(ctx, status.ID, db); err != nil {
			status.HookStatus = "FAILED"
			return statuses, err
		}
		status.HookStatus = "PASSED"
	}
	var foreignKeyViolations int64
	if err := db.Raw("SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&foreignKeyViolations).Error; err != nil || foreignKeyViolations != 0 {
		return statuses, errors.Join(err, errors.New("staged restore foreign-key postcondition failed"))
	}
	return statuses, nil
}

func cloneRestoreFile(ctx context.Context, sourcePath, destinationPath string) error {
	source, err := os.Open(sourcePath) // #nosec G304 -- bounded internal staging path.
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := copyContext(ctx, destination, io.LimitReader(source, MaxRestoreBytes+1))
	syncErr, closeErr := destination.Sync(), destination.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || written <= 0 || written > MaxRestoreBytes {
		return errors.Join(copyErr, syncErr, closeErr, errors.New("staged migration copy is invalid"))
	}
	return nil
}

func restoreRehearsalRevision(result RestoreRehearsal) string {
	result.Revision, result.GeneratedAt = "", 0
	data, _ := json.Marshal(result)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func uniqueStrings(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
