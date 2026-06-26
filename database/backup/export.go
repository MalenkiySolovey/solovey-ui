package backup

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type backupTable struct {
	name  string
	model any
}

func backupTables() []backupTable {
	return []backupTable{
		{name: "settings", model: &model.Setting{}},
		{name: "tls", model: &model.Tls{}},
		{name: "inbounds", model: &model.Inbound{}},
		{name: "outbounds", model: &model.Outbound{}},
		{name: "remote_outbound_subscriptions", model: &model.RemoteOutboundSubscription{}},
		{name: "remote_outbound_groups", model: &model.RemoteOutboundGroup{}},
		{name: "remote_outbound_group_connections", model: &model.RemoteOutboundGroupConnection{}},
		{name: "remote_outbound_connections", model: &model.RemoteOutboundConnection{}},
		{name: "services", model: &model.Service{}},
		{name: "endpoints", model: &model.Endpoint{}},
		{name: "users", model: &model.User{}},
		{name: "tokens", model: &model.Tokens{}},
		{name: "stats", model: &model.Stats{}},
		{name: "client_ips", model: &model.ClientIP{}},
		{name: "clients", model: &model.Client{}},
		{name: "changes", model: &model.Changes{}},
		{name: "audit_events", model: &model.AuditEvent{}},
	}
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
	excludedTables := parseBackupExcludes(exclude)
	dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
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

	tables := backupTables()
	models := make([]any, 0, len(tables))
	for _, table := range tables {
		models = append(models, table.model)
	}
	if err = backupDB.AutoMigrate(models...); err != nil {
		return "", nil, err
	}

	sourceDB := dbsqlite.DB()
	if sourceDB == nil {
		return "", nil, common.NewError("database is not initialized")
	}
	for _, table := range tables {
		if excludedTables[table.name] {
			continue
		}
		tableSource := sourceDB
		if table.name == "tls" {
			tableSource = sourceDB.Where("id <> ?", 0)
		}
		if err := copyBackupTable(tableSource, backupDB, table.model); err != nil {
			return "", nil, err
		}
	}
	if err := dbsqlite.EnsureNoTLSRow(backupDB); err != nil {
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
			return tx.CreateInBatches(slicePtr.Elem().Interface(), batch).Error
		}).Error
	})
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
