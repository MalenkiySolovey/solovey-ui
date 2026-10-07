package migration

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	validation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRuleUpgradePreflightPreservesStoredPreimage(t *testing.T) {
	for _, name := range []string{"route-and", "dns-or", "route-conflicting", "dns-action-only"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "internal", "singbox", "validation", "testdata", "nested_rules_v2026.3.3", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "upgrade.db")
		db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("CREATE TABLE settings (key text, value text)").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("INSERT INTO settings (key,value) VALUES (?,?)", "config", string(data)).Error; err != nil {
			t.Fatal(err)
		}
		_, expected := validation.PrepareRuleUpgrade(data)
		if expected != nil {
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := MigratePath(path, Options{}); err == nil {
				t.Fatal("full upgrader accepted a manual rule before cutover")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(before) != string(after) {
				t.Fatal("failed full migration changed the source file", err)
			}
		}
		for attempt := 0; attempt < 2; attempt++ {
			err := readOnlyRuleUpgradePreflight(path)
			if (err != nil) != (expected != nil) {
				t.Fatal("preflight differs from owner", err, expected)
			}
			if err != nil {
				var actual, want *validation.RuleConditionError
				if !errors.As(err, &actual) || !errors.As(expected, &want) || actual.Finding.Code != want.Finding.Code || actual.Finding.Path != want.Finding.Path {
					t.Fatal("preflight lost stable reason/path")
				}
			}
			var stored string
			if err := db.Table("settings").Select("value").Where("key = ?", "config").Scan(&stored).Error; err != nil || stored != string(data) {
				t.Fatal("upgrade changed original state", err)
			}
		}
		connection, _ := db.DB()
		_ = connection.Close()
	}
}
