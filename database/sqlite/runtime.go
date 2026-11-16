package sqlite

import (
	"context"
	storagesqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"gorm.io/gorm"
)

// Compatibility forwarding retains Product bootstrap callers. The Backend
// package is the only connection, admission and maintenance implementation.
type Maintenance = storagesqlite.Maintenance
type RuntimeStatus = storagesqlite.RuntimeStatus

var (
	ErrMaintenance = storagesqlite.ErrMaintenance
	ErrRetired     = storagesqlite.ErrRetired
)

const (
	SQLiteModuleVersion   = storagesqlite.SQLiteModuleVersion
	SQLiteModuleCommit    = storagesqlite.SQLiteModuleCommit
	SQLiteModuleSum       = storagesqlite.SQLiteModuleSum
	SQLiteRuntimeVersion  = storagesqlite.SQLiteRuntimeVersion
	SQLiteRuntimeSourceID = storagesqlite.SQLiteRuntimeSourceID
	WALResetSafetyClass   = storagesqlite.WALResetSafetyClass
)

func DB() *gorm.DB                               { return storagesqlite.DB() }
func Close() error                               { return storagesqlite.Close() }
func CloseContext(ctx context.Context) error     { return storagesqlite.CloseContext(ctx) }
func CloseForFileSwap(ctx context.Context) error { return storagesqlite.CloseForFileSwap(ctx) }
func AcquireOperation(ctx context.Context) (context.Context, func(), error) {
	return storagesqlite.AcquireOperation(ctx)
}
func ReleaseOperation(ctx context.Context) { storagesqlite.ReleaseOperation(ctx) }
func BeginMaintenance(ctx context.Context) (*Maintenance, error) {
	return storagesqlite.BeginMaintenance(ctx)
}
func IsMaintenanceContext(ctx context.Context) bool { return storagesqlite.IsMaintenanceContext(ctx) }
func IsPrivateDatabase(ctx context.Context, database *gorm.DB) bool {
	return storagesqlite.IsPrivateDatabase(ctx, database)
}
func IsOpen() bool                                 { return storagesqlite.IsOpen() }
func IsNotFound(err error) bool                    { return storagesqlite.IsNotFound(err) }
func BatchSize(database *gorm.DB, model any) int   { return storagesqlite.BatchSize(database, model) }
func CreateInBatches(tx *gorm.DB, slice any) error { return storagesqlite.CreateInBatches(tx, slice) }
func SaveInBatches(tx *gorm.DB, slice any) error   { return storagesqlite.SaveInBatches(tx, slice) }
func ReserveWrite(tx *gorm.DB) error               { return storagesqlite.ReserveWrite(tx) }
func InspectRuntime(database *gorm.DB) (RuntimeStatus, error) {
	return storagesqlite.InspectRuntime(database)
}
func ParseSQLiteVersion(value string) int { return storagesqlite.ParseSQLiteVersion(value) }
func OpenReadOnly(ctx context.Context, path string) (*gorm.DB, error) {
	return storagesqlite.OpenReadOnly(ctx, path)
}
