package steps

import (
	"fmt"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPanelSecurityMigrationIsAdditiveIdempotentAndPreservesLegacyAccess(t *testing.T) {
	dsn := fmt.Sprintf("file:test-security-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := db.Exec(`
CREATE TABLE users (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	sort_order INTEGER NOT NULL DEFAULT 0,
	username TEXT,
	password TEXT,
	last_logins TEXT,
	force_password_reset NUMERIC NOT NULL DEFAULT 0
)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`
CREATE TABLE settings (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	key TEXT,
	value TEXT
)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(
		"INSERT INTO users(username,password,force_password_reset) VALUES(?,?,?)",
		"legacy-admin",
		"legacy-hash-placeholder",
		false,
	).Error; err != nil {
		t.Fatal(err)
	}

	if err := addPanelNativeSecuritySchema(db); err != nil {
		t.Fatal(err)
	}
	if err := addPanelNativeSecuritySchema(db); err != nil {
		t.Fatalf("migration is not idempotent: %v", err)
	}
	for _, table := range []any{
		&model.AdminMFAFactor{},
		&model.AdminRecoveryCode{},
		&model.SecuritySession{},
		&model.StepUpGrant{},
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("missing panel-security table for %T", table)
		}
	}
	var user model.User
	if err := db.Where("username = ?", "legacy-admin").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if user.CredentialGeneration != 1 || user.MFAGeneration != 1 {
		t.Fatalf("security generations were not backfilled: %#v", user)
	}
	if user.ForcePasswordReset || user.PasswordPolicyVersion != 0 {
		t.Fatalf("legacy access was silently restricted: %#v", user)
	}
	var lifetimePolicy string
	if err := db.Raw("SELECT value FROM settings WHERE key = ?", "sessionLifetimePolicy").Scan(&lifetimePolicy).Error; err != nil {
		t.Fatal(err)
	}
	if lifetimePolicy != "legacy_unbounded" {
		t.Fatalf("legacy session policy=%q, want legacy_unbounded", lifetimePolicy)
	}
	if !db.Migrator().HasColumn(&model.SecuritySession{}, "LastMFAAt") {
		t.Fatal("security_sessions.last_mfa_at was not created")
	}
}
