package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	componentmanifest "github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/durableowner"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type backupContributionRow struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}

func (backupContributionRow) TableName() string {
	return "component_backup_rows"
}

func TestExportIncludesRegisteredComponentTables(t *testing.T) {
	dbPath := initBackupContributionDB(t)
	owner := componentmanifest.Manifest{
		ID: "test-component", Name: "Test component", Version: "1", Delivery: componentmanifest.DeliveryInProcess,
		Database: componentmanifest.Database{Tables: []string{"component_backup_rows"}},
	}.Normalized()
	durableowner.Register(owner)
	installedPath := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installedPath)
	if err := installstate.Store(installedPath, installstate.Metadata{Version: 1, Components: []installstate.InstalledComponent{{
		ID: owner.ID, Delivery: owner.Delivery, Installed: true,
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().AutoMigrate(&backupContributionRow{}); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&backupContributionRow{Name: "registered"}).Error; err != nil {
		t.Fatal(err)
	}

	unregister := RegisterTables("test-component", []TableContribution{
		{Name: "component_backup_rows", Model: &backupContributionRow{}},
	})
	t.Cleanup(unregister)

	backupDB := openContributionBackup(t, dbPath)
	var count int64
	if err := backupDB.Model(&backupContributionRow{}).Where("name = ?", "registered").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("registered component backup rows=%d, want 1", count)
	}
}

func TestExportSkipsUnregisteredComponentTables(t *testing.T) {
	dbPath := initBackupContributionDB(t)
	if err := dbsqlite.DB().AutoMigrate(&backupContributionRow{}); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&backupContributionRow{Name: "unregistered"}).Error; err != nil {
		t.Fatal(err)
	}

	backupDB := openContributionBackup(t, dbPath)
	if backupDB.Migrator().HasTable("component_backup_rows") {
		t.Fatal("unregistered component table leaked into backup")
	}
}

func initBackupContributionDB(t *testing.T) string {
	t.Helper()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "s-ui.db")
	t.Setenv("SUI_DB_FOLDER", dbDir)
	if err := dbsqlite.Init(dbPath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeMainDB(t)
		cleanupBackupSidecars(dbPath)
	})
	return dbPath
}

func openContributionBackup(t *testing.T, liveDBPath string) *gorm.DB {
	t.Helper()
	backup, err := Export("")
	if err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(t.TempDir(), "backup.db")
	if err := os.WriteFile(backupPath, backup, 0o600); err != nil {
		t.Fatal(err)
	}
	backupDB, err := gorm.Open(sqlite.Open(backupPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := backupDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		cleanupBackupSidecars(backupPath)
		cleanupBackupSidecars(liveDBPath)
	})
	return backupDB
}
