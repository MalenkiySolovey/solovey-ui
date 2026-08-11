package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/database/sqliteident"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type backupTable struct {
	name          string
	model         any
	owner         string
	opaque        bool
	alwaysExclude bool
	exclusionCode string
}

func backupTables() []backupTable {
	return []backupTable{
		{name: "settings", model: &model.Setting{}},
		{name: "tls", model: &model.Tls{}},
		{name: "inbounds", model: &model.Inbound{}},
		{name: "inbound_drafts", model: &model.InboundDraft{}},
		{name: "outbounds", model: &model.Outbound{}},
		{name: "services", model: &model.Service{}},
		{name: "endpoints", model: &model.Endpoint{}},
		{name: "users", model: &model.User{}},
		{name: "admin_mfa_factors", model: &model.AdminMFAFactor{}},
		{name: "admin_recovery_codes", model: &model.AdminRecoveryCode{}},
		{name: "security_sessions", model: &model.SecuritySession{}, alwaysExclude: true, exclusionCode: "NONPORTABLE_RUNTIME_AUTHORITY"},
		{name: "step_up_grants", model: &model.StepUpGrant{}, alwaysExclude: true, exclusionCode: "NONPORTABLE_RUNTIME_AUTHORITY"},
		{name: "tokens", model: &model.Tokens{}},
		{name: "stats", model: &model.Stats{}},
		{name: "client_ips", model: &model.ClientIP{}},
		{name: "clients", model: &model.Client{}},
		{name: "changes", model: &model.Changes{}},
		{name: "audit_events", model: &model.AuditEvent{}},
		{name: "inbound_fallback_checkpoints", model: &model.InboundFallbackCheckpoint{}},
		{name: "inbound_endpoint_leases", model: &model.InboundEndpointLease{}, alwaysExclude: true, exclusionCode: "NONPORTABLE_RUNTIME_AUTHORITY"},
		{name: "failover_state", model: &model.FailoverMemberState{}, alwaysExclude: true, exclusionCode: "NONPORTABLE_RUNTIME_STATE"},
		{name: "component_migrations", model: &model.ComponentMigration{}},
		// Safe SSH semantic metadata is portable. Exact artifact checkpoints and
		// reconnect verifier digests are intentionally absent from backups.
		{name: "ssh_posture_snapshots_v1", model: &model.SSHPostureSnapshot{}},
		{name: "ssh_management_candidates_v1", model: &model.SSHManagementCandidate{}},
		{name: "ssh_recovery_evidence_v1", model: &model.SSHRecoveryEvidence{}},
		{name: "ssh_management_journal_v1", model: &model.SSHManagementJournal{}},
		{name: "ssh_managed_artifact_checkpoints_v1", model: &model.SSHManagedArtifactCheckpoint{}, alwaysExclude: true, exclusionCode: "NONPORTABLE_HOST_AUTHORITY"},
		{name: "ssh_reconnect_challenges_v1", model: &model.SSHReconnectChallenge{}, alwaysExclude: true, exclusionCode: "NONPORTABLE_RUNTIME_AUTHORITY"},
		// Deployment profile/doctor/timeline metadata is portable. Broker
		// checkpoint references and receipts are scrubbed while copying below.
		{name: "deployment_state_v1", model: &model.DeploymentState{}},
		{name: "deployment_operations_v1", model: &model.DeploymentOperation{}},
		{name: "deployment_journal_v1", model: &model.DeploymentJournal{}},
		{name: "deployment_doctor_snapshots_v1", model: &model.DeploymentDoctorSnapshot{}},
		{name: "update_release_state_v1", model: &model.UpdateReleaseState{}},
		{name: "update_operations_v1", model: &model.UpdateOperation{}},
		{name: "update_journal_v1", model: &model.UpdateJournal{}},
		{name: "resource_pressure_state_v1", model: &model.ResourcePressureState{}},
		{name: "resource_pressure_transitions_v1", model: &model.ResourcePressureTransition{}},
		{name: "migration_journal_v1", model: &model.MigrationJournal{}},
		{name: "data_lifecycle_operations_v1", model: &model.DataLifecycleOperation{}},
		{name: "data_lifecycle_journal_v1", model: &model.DataLifecycleJournal{}},
	}
}

func exportTables(sourceDB *gorm.DB) ([]backupTable, error) {
	tables := backupTables()
	seen := make(map[string]struct{}, len(tables))
	for _, table := range tables {
		seen[table.name] = struct{}{}
	}
	for _, table := range contributedBackupTables(sourceDB) {
		if _, ok := seen[table.name]; ok {
			continue
		}
		seen[table.name] = struct{}{}
		tables = append(tables, table)
	}
	opaque, err := installedOwnerTables(sourceDB, seen)
	if err != nil {
		return nil, err
	}
	tables = append(tables, opaque...)
	return tables, nil
}

// Export returns a self-contained SQLite backup of the selected tables.
func Export(exclude string) ([]byte, error) {
	dbPath, cleanup, err := PrepareExport(exclude)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	file, err := os.Open(dbPath) // #nosec G304 -- internal temporary path.
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

// PrepareExport creates a self-contained SQLite backup file and returns its
// path plus a cleanup callback. Callers that can stream a file should use this
// instead of Export to avoid holding the entire database in memory.
func PrepareExport(exclude string) (string, func(), error) {
	return PrepareExportContext(context.Background(), exclude)
}

func PrepareExportContext(ctx context.Context, exclude string) (string, func(), error) {
	if ctx == nil {
		return "", nil, errors.New("backup context is required")
	}
	excludedTables := parseBackupExcludes(exclude)
	dir := configstorage.GetDBFolderPath()
	if dir == "" {
		return "", nil, errors.New("backup staging directory is unavailable")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, err
	}
	tmpFile, err := os.CreateTemp(dir, "s-ui-backup-*.db")
	if err != nil {
		return "", nil, err
	}
	dbPath := tmpFile.Name()
	cleanup := func() { cleanupBackupTempFiles(dbPath) }
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			cleanup()
		}
	}()
	if err := tmpFile.Close(); err != nil {
		return "", nil, err
	}
	if backupTempPathHook != nil {
		backupTempPathHook(dbPath)
	}

	backupDB, err := gorm.Open(gormsqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return "", nil, err
	}
	backupSQLDB, err := backupDB.DB()
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = backupSQLDB.Close() }()

	sourceDB := dbsqlite.DB()
	if sourceDB == nil {
		return "", nil, common.NewError("database is not initialized")
	}
	tables, err := exportTables(sourceDB)
	if err != nil {
		return "", nil, err
	}
	models := make([]any, 0, len(tables))
	for _, table := range tables {
		if table.model != nil && !table.alwaysExclude {
			models = append(models, table.model)
		}
	}
	if err = backupDB.AutoMigrate(models...); err != nil {
		return "", nil, err
	}

	for _, table := range tables {
		if table.alwaysExclude {
			continue
		}
		if excludedTables[table.name] {
			continue
		}
		tableSource := sourceDB
		if table.name == "tls" {
			tableSource = sourceDB.Where("id <> ?", 0)
		}
		if table.opaque {
			err = copyOpaqueTable(ctx, tableSource, backupDB, table.name)
		} else {
			err = copyBackupTable(tableSource, backupDB, table.model)
		}
		if err != nil {
			return "", nil, err
		}
	}
	if err := dbsqlite.EnsureNoTLSRow(backupDB); err != nil {
		return "", nil, err
	}
	if err := writeBackupManifest(ctx, backupDB, tables, excludedTables); err != nil {
		return "", nil, err
	}
	if err := walCheckpointWithFallback(backupDB); err != nil {
		logger.Warning("backup WAL checkpoint failed in both TRUNCATE and FULL modes: ", err, "; continuing without checkpoint")
	}
	if err := backupSQLDB.Close(); err != nil {
		return "", nil, err
	}
	cleanupBackupSidecars(dbPath)

	cleanupOnError = false
	return dbPath, cleanup, nil
}

func parseBackupExcludes(exclude string) map[string]bool {
	excluded := map[string]bool{}
	for _, table := range strings.Split(exclude, ",") {
		switch table = strings.TrimSpace(table); table {
		case "audit":
			excluded["audit_events"] = true
		case "audit_events", "client_ips", "changes", "stats":
			excluded[table] = true
		}
	}
	return excluded
}

func ParseExcludes(exclude string) []string {
	excluded := parseBackupExcludes(exclude)
	ordered := make([]string, 0, len(excluded))
	for _, table := range []string{"stats", "client_ips", "audit_events", "changes"} {
		if excluded[table] {
			ordered = append(ordered, table)
		}
	}
	return ordered
}

func copyBackupTable(sourceDB *gorm.DB, backupDB *gorm.DB, modelValue any) error {
	modelType := reflect.TypeOf(modelValue)
	if modelType.Kind() != reflect.Ptr {
		return common.NewError("backup model must be a pointer")
	}
	batch := dbsqlite.BatchSize(backupDB, modelValue)
	return backupDB.Transaction(func(tx *gorm.DB) error {
		slicePtr := reflect.New(reflect.SliceOf(modelType.Elem()))
		return sourceDB.Model(modelValue).FindInBatches(slicePtr.Interface(), batch, func(_ *gorm.DB, _ int) error {
			if slicePtr.Elem().Len() == 0 {
				return nil
			}
			if _, deploymentOperations := modelValue.(*model.DeploymentOperation); deploymentOperations {
				for index := 0; index < slicePtr.Elem().Len(); index++ {
					row := slicePtr.Elem().Index(index).Addr().Interface().(*model.DeploymentOperation)
					row.CheckpointRef = ""
					row.BrokerReceipt = ""
					row.RestoredUntrusted = true
				}
			}
			if _, updateStates := modelValue.(*model.UpdateReleaseState); updateStates {
				for index := 0; index < slicePtr.Elem().Len(); index++ {
					row := slicePtr.Elem().Index(index).Addr().Interface().(*model.UpdateReleaseState)
					row.LastVerifiedSequence, row.ManifestDigest, row.SigningKeyID = 0, "", ""
				}
			}
			if _, updateOperations := modelValue.(*model.UpdateOperation); updateOperations {
				for index := 0; index < slicePtr.Elem().Len(); index++ {
					row := slicePtr.Elem().Index(index).Addr().Interface().(*model.UpdateOperation)
					row.RestoredUntrusted = true
				}
			}
			if _, pressureStates := modelValue.(*model.ResourcePressureState); pressureStates {
				for index := 0; index < slicePtr.Elem().Len(); index++ {
					row := slicePtr.Elem().Index(index).Addr().Interface().(*model.ResourcePressureState)
					row.PreviousState, row.State, row.ReasonCode = row.State, "UNKNOWN", "restored_pressure_state_untrusted"
				}
			}
			if _, dataOperations := modelValue.(*model.DataLifecycleOperation); dataOperations {
				for index := 0; index < slicePtr.Elem().Len(); index++ {
					row := slicePtr.Elem().Index(index).Addr().Interface().(*model.DataLifecycleOperation)
					row.RestoredUntrusted = true
				}
			}
			return tx.CreateInBatches(slicePtr.Elem().Interface(), batch).Error
		}).Error
	})
}

func copyOpaqueTable(ctx context.Context, sourceDB, backupDB *gorm.DB, tableName string) error {
	if ctx == nil || sourceDB == nil || backupDB == nil || !sqliteident.Valid(tableName) {
		return errors.New("opaque backup table request is invalid")
	}
	var schemaSQL string
	if err := sourceDB.WithContext(ctx).Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ? LIMIT 1", tableName).Scan(&schemaSQL).Error; err != nil {
		return err
	}
	if schemaSQL == "" || len(schemaSQL) > 256<<10 {
		return fmt.Errorf("opaque backup schema for %s is unavailable", tableName)
	}
	if err := backupDB.WithContext(ctx).Exec(schemaSQL).Error; err != nil {
		return err
	}
	rows, err := sourceDB.WithContext(ctx).Raw("SELECT * FROM " + sqliteident.Quote(tableName)).Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil || len(columns) == 0 || len(columns) > 256 {
		return errors.New("opaque backup column inventory is invalid")
	}
	for _, column := range columns {
		if !sqliteident.Valid(column) {
			return errors.New("opaque backup column identity is invalid")
		}
	}
	quotedColumns := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for index, column := range columns {
		quotedColumns[index], placeholders[index] = sqliteident.Quote(column), "?"
	}
	insertSQL := "INSERT INTO " + sqliteident.Quote(tableName) + " (" + strings.Join(quotedColumns, ",") + ") VALUES (" + strings.Join(placeholders, ",") + ")"
	if err := backupDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for rows.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}
			values := make([]any, len(columns))
			targets := make([]any, len(columns))
			for index := range values {
				targets[index] = &values[index]
			}
			if err := rows.Scan(targets...); err != nil {
				return err
			}
			if err := tx.Exec(insertSQL, values...).Error; err != nil {
				return err
			}
		}
		return rows.Err()
	}); err != nil {
		return err
	}
	var definitions []string
	if err := sourceDB.WithContext(ctx).Raw("SELECT sql FROM sqlite_master WHERE tbl_name = ? AND type IN ('index','trigger') AND sql IS NOT NULL ORDER BY type, name", tableName).Scan(&definitions).Error; err != nil {
		return err
	}
	if len(definitions) > 256 {
		return errors.New("opaque backup secondary schema is too large")
	}
	for _, definition := range definitions {
		if definition == "" || len(definition) > 256<<10 {
			return errors.New("opaque backup secondary schema is invalid")
		}
		if err := backupDB.WithContext(ctx).Exec(definition).Error; err != nil {
			return err
		}
	}
	return nil
}

var backupTempPathHook func(string)

func cleanupBackupTempFiles(dbPath string) {
	_ = os.Remove(dbPath)
	cleanupBackupSidecars(dbPath)
}

func cleanupBackupSidecars(dbPath string) {
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	_ = os.Remove(dbPath + "-journal")
}

func walCheckpointWithFallback(db *gorm.DB) error {
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE);").Error; err != nil {
		if fallbackErr := db.Exec("PRAGMA wal_checkpoint(FULL);").Error; fallbackErr != nil {
			return fallbackErr
		}
		logger.Warning("backup WAL TRUNCATE checkpoint failed, fell back to FULL: ", err)
	}
	return nil
}
