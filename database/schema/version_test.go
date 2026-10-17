package schema

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAdditiveClientSchemaDoesNotChangeStoredRuntimeProjection(t *testing.T) {
	for _, tc := range []struct {
		version          string
		legacy, rejected bool
	}{
		{"1.11", true, false}, {"1.12", false, false}, {CurrentCoreVersion, false, false}, {"99.0", false, true},
	} {
		t.Run(tc.version, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
			if err := db.Exec("CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT)").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("INSERT INTO settings VALUES ('coreSchemaVersion',?)", tc.version).Error; err != nil {
				t.Fatal(err)
			}
			legacy, err := LegacyProjection(db)
			if (err != nil) != tc.rejected || legacy != tc.legacy {
				t.Fatalf("projection of %s: legacy=%v err=%v", tc.version, legacy, err)
			}
		})
	}
}
