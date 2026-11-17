package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/sqliteident"

	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Table struct {
	Name          string
	Model         any
	Owner         string
	Opaque        bool
	AlwaysExclude bool
	ExclusionCode string
}

// Exporter coordinates a private logical archive through the canonical SQLite lifetime.
// Product supplies release, installed-owner and semantic file/TLS owner inputs.
type Exporter struct {
	StagingDirectory func() string
	AppVersion       func() string
	InstalledOwners  func() ([]string, error)
	EnsureSentinel   func(*gorm.DB) error
	ExportOwnerFiles func(context.Context, *gorm.DB, *gorm.DB) (*FileBackupManifest, error)
	Warning          func(...any)
}

func (e Exporter) warn(values ...any) {
	if e.Warning != nil {
		e.Warning(values...)
	}
}
func (e Exporter) ensureSentinel(db *gorm.DB) error {
	if e.EnsureSentinel == nil {
		return errors.New("backup TLS owner is unavailable")
	}
	return e.EnsureSentinel(db)
}
func (e Exporter) exportOwnerFiles(ctx context.Context, source, destination *gorm.DB) (*FileBackupManifest, error) {
	if e.ExportOwnerFiles == nil {
		return nil, errors.New("logical file owners unavailable")
	}
	return e.ExportOwnerFiles(ctx, source, destination)
}
func (e Exporter) writeBackupManifest(ctx context.Context, db *gorm.DB, tables []Table, excluded map[string]bool, files ...*FileBackupManifest) error {
	selected := make([]ManifestTable, len(tables))
	for index, table := range tables {
		selected[index] = ManifestTable{Name: table.Name, Owner: table.Owner, Opaque: table.Opaque, AlwaysExclude: table.AlwaysExclude, ExclusionCode: table.ExclusionCode}
	}
	if e.AppVersion == nil {
		return errors.New("backup release identity is unavailable")
	}
	return WriteManifest(ctx, db, selected, excluded, ManifestFacts{AppVersion: e.AppVersion(), InstalledOwners: e.InstalledOwners}, files...)
}

func CoreTables() []Table {
	return []Table{
		{Name: "settings", Model: &model.Setting{}},
		{Name: "tls", Model: &model.Tls{}},
		{Name: "tls_certificate_providers", Model: &model.TLSCertificateProvider{}},
		{Name: "inbounds", Model: &model.Inbound{}},
		{Name: "inbound_drafts", Model: &model.InboundDraft{}},
		{Name: "outbounds", Model: &model.Outbound{}},
		{Name: "services", Model: &model.Service{}},
		{Name: "endpoints", Model: &model.Endpoint{}},
		{Name: "users", Model: &model.User{}},
		{Name: "admin_mfa_factors", Model: &model.AdminMFAFactor{}},
		{Name: "admin_recovery_codes", Model: &model.AdminRecoveryCode{}},
		{Name: "security_sessions", Model: &model.SecuritySession{}, AlwaysExclude: true, ExclusionCode: "NONPORTABLE_RUNTIME_AUTHORITY"},
		{Name: "step_up_grants", Model: &model.StepUpGrant{}, AlwaysExclude: true, ExclusionCode: "NONPORTABLE_RUNTIME_AUTHORITY"},
		{Name: "tokens", Model: &model.Tokens{}},
		{Name: "stats", Model: &model.Stats{}},
		{Name: "client_ips", Model: &model.ClientIP{}},
		{Name: "clients", Model: &model.Client{}},
		{Name: "changes", Model: &model.Changes{}},
		{Name: "audit_events", Model: &model.AuditEvent{}},
		{Name: "inbound_fallback_checkpoints", Model: &model.InboundFallbackCheckpoint{}},
		{Name: "inbound_endpoint_leases", Model: &model.InboundEndpointLease{}, AlwaysExclude: true, ExclusionCode: "NONPORTABLE_RUNTIME_AUTHORITY"},
		{Name: "failover_state", Model: &model.FailoverMemberState{}, AlwaysExclude: true, ExclusionCode: "NONPORTABLE_RUNTIME_STATE"},
		{Name: "component_migrations", Model: &model.ComponentMigration{}},
		// Safe SSH semantic metadata is portable. Exact artifact checkpoints and
		// reconnect verifier digests are intentionally absent from backups.
		{Name: "ssh_posture_snapshots_v1", Model: &model.SSHPostureSnapshot{}},
		{Name: "ssh_management_candidates_v1", Model: &model.SSHManagementCandidate{}},
		{Name: "ssh_recovery_evidence_v1", Model: &model.SSHRecoveryEvidence{}},
		{Name: "ssh_management_journal_v1", Model: &model.SSHManagementJournal{}},
		{Name: "ssh_managed_artifact_checkpoints_v1", Model: &model.SSHManagedArtifactCheckpoint{}, AlwaysExclude: true, ExclusionCode: "NONPORTABLE_HOST_AUTHORITY"},
		{Name: "ssh_reconnect_challenges_v1", Model: &model.SSHReconnectChallenge{}, AlwaysExclude: true, ExclusionCode: "NONPORTABLE_RUNTIME_AUTHORITY"},
		// Deployment profile/doctor/timeline metadata is portable. Broker
		// checkpoint references and receipts are scrubbed while copying below.
		{Name: "deployment_state_v1", Model: &model.DeploymentState{}},
		{Name: "deployment_operations_v1", Model: &model.DeploymentOperation{}},
		{Name: "deployment_journal_v1", Model: &model.DeploymentJournal{}},
		{Name: "deployment_doctor_snapshots_v1", Model: &model.DeploymentDoctorSnapshot{}},
		{Name: "update_release_state_v1", Model: &model.UpdateReleaseState{}},
		{Name: "update_operations_v1", Model: &model.UpdateOperation{}},
		{Name: "update_journal_v1", Model: &model.UpdateJournal{}},
		{Name: "resource_pressure_state_v1", Model: &model.ResourcePressureState{}},
		{Name: "resource_pressure_transitions_v1", Model: &model.ResourcePressureTransition{}},
		{Name: "migration_journal_v1", Model: &model.MigrationJournal{}},
		{Name: "data_lifecycle_operations_v1", Model: &model.DataLifecycleOperation{}},
		{Name: "data_lifecycle_journal_v1", Model: &model.DataLifecycleJournal{}},
	}
}

func (e Exporter) exportTables(sourceDB *gorm.DB) ([]Table, error) {
	tables := CoreTables()
	seen := make(map[string]struct{}, len(tables))
	for _, table := range tables {
		seen[table.Name] = struct{}{}
	}
	owned, err := e.installedOwnerTables(sourceDB, seen)
	if err != nil {
		return nil, err
	}
	tables = append(tables, owned...)
	return tables, nil
}

// Export returns a self-contained SQLite backup of the selected tables.
func (e Exporter) Export(exclude string) ([]byte, error) {
	dbPath, cleanup, err := e.PrepareExport(context.Background(), exclude)
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
func (e Exporter) PrepareExport(ctx context.Context, exclude string) (string, func(), error) {
	if e.StagingDirectory == nil {
		return "", nil, errors.New("backup staging directory is unavailable")
	}
	return e.PrepareExportInDirectory(ctx, exclude, e.StagingDirectory())
}

// PrepareExportContextInDirectory lets a recovery owner keep its logical export
// on the filesystem required by its atomic publication operation. The directory
// is a trusted owner input, never a path from the backup being restored.
func (e Exporter) PrepareExportInDirectory(ctx context.Context, exclude, dir string) (string, func(), error) {
	if ctx == nil {
		return "", nil, errors.New("backup context is required")
	}
	ctx, release, err := dbsqlite.AcquireOperation(ctx)
	if err != nil {
		return "", nil, err
	}
	defer release()
	excludedTables := parseBackupExcludes(exclude)
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
		return "", nil, errors.New("database is not initialized")
	}
	sourceSnapshot := sourceDB.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true})
	if sourceSnapshot.Error != nil {
		return "", nil, sourceSnapshot.Error
	}
	defer func() { _ = sourceSnapshot.Rollback().Error }()
	// Force SQLite to establish the read snapshot before any table is copied.
	// Every subsequent source query therefore observes one database revision.
	var snapshotMarker int
	if err := sourceSnapshot.Raw("SELECT COUNT(*) FROM sqlite_master").Scan(&snapshotMarker).Error; err != nil {
		return "", nil, err
	}
	tables, err := e.exportTables(sourceSnapshot)
	if err != nil {
		return "", nil, err
	}
	models := make([]any, 0, len(tables))
	for _, table := range tables {
		if table.Model != nil && !table.AlwaysExclude {
			models = append(models, table.Model)
		}
	}
	if err = backupDB.AutoMigrate(models...); err != nil {
		return "", nil, err
	}

	for _, table := range tables {
		if table.AlwaysExclude {
			continue
		}
		if excludedTables[table.Name] {
			continue
		}
		tableSource := sourceSnapshot
		if table.Name == "tls" {
			tableSource = sourceSnapshot.Where("id <> ?", 0)
		}
		if table.Opaque {
			err = copyOpaqueTable(ctx, tableSource, backupDB, table.Name)
		} else {
			err = copyBackupTable(tableSource, backupDB, table.Model)
		}
		if err != nil {
			return "", nil, err
		}
	}
	if err := e.ensureSentinel(backupDB); err != nil {
		return "", nil, err
	}
	files, err := e.exportOwnerFiles(ctx, sourceSnapshot, backupDB)
	if err != nil {
		return "", nil, err
	}
	if err := e.writeBackupManifest(ctx, backupDB, tables, excludedTables, files); err != nil {
		return "", nil, err
	}
	if err := e.walCheckpointWithFallback(backupDB); err != nil {
		e.warn("backup WAL checkpoint failed in both TRUNCATE and FULL modes: ", err, "; continuing without checkpoint")
	}
	if err := backupSQLDB.Close(); err != nil {
		return "", nil, err
	}
	CleanupSidecars(dbPath)

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
		return errors.New("backup model must be a pointer")
	}
	batch := dbsqlite.BatchSize(backupDB, modelValue)
	statement := &gorm.Statement{DB: backupDB}
	if err := statement.Parse(modelValue); err != nil {
		return err
	}
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
					row.CheckpointReleased = true
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
			// These are stored facts, not new resources. Struct creation would
			// replace zero values with model defaults and generate timestamps.
			// Explicit columns retain those values after the owner-specific
			// authority scrubbing above, including registered serializers.
			rows := make([]map[string]any, slicePtr.Elem().Len())
			for index := range rows {
				rows[index] = make(map[string]any)
				for _, field := range statement.Schema.Fields {
					if field.DBName == "" || !field.Readable || !field.Creatable {
						continue
					}
					value, _ := field.ValueOf(sourceDB.Statement.Context, slicePtr.Elem().Index(index))
					rows[index][field.DBName] = value
				}
			}
			return tx.Table(statement.Table).CreateInBatches(rows, batch).Error
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
	var triggerCount int64
	if err := sourceDB.WithContext(ctx).Raw("SELECT COUNT(*) FROM sqlite_master WHERE tbl_name = ? AND type = 'trigger'", tableName).Scan(&triggerCount).Error; err != nil {
		return err
	}
	if triggerCount != 0 {
		return errors.New("opaque backup table contains unsupported executable triggers")
	}
	var definitions []string
	if err := sourceDB.WithContext(ctx).Raw("SELECT sql FROM sqlite_master WHERE tbl_name = ? AND type = 'index' AND sql IS NOT NULL ORDER BY name", tableName).Scan(&definitions).Error; err != nil {
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

func cleanupBackupTempFiles(dbPath string) {
	_ = os.Remove(dbPath)
	CleanupSidecars(dbPath)
}

func CleanupSidecars(dbPath string) {
	_ = os.Remove(dbPath + "-wal")
	_ = os.Remove(dbPath + "-shm")
	_ = os.Remove(dbPath + "-journal")
}

func (e Exporter) walCheckpointWithFallback(db *gorm.DB) error {
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE);").Error; err != nil {
		if fallbackErr := db.Exec("PRAGMA wal_checkpoint(FULL);").Error; fallbackErr != nil {
			return fallbackErr
		}
		e.warn("backup WAL TRUNCATE checkpoint failed, fell back to FULL: ", err)
	}
	return nil
}
