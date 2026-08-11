package steps

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	"gorm.io/gorm"
)

func addPanelNativeSecuritySchema(tx *gorm.DB) error {
	if !tx.Migrator().HasTable(&model.User{}) {
		if err := tx.AutoMigrate(&model.User{}); err != nil {
			return err
		}
	} else {
		var columns []struct {
			Name string `gorm:"column:name"`
		}
		if err := tx.Raw("PRAGMA table_info(users)").Scan(&columns).Error; err != nil {
			return err
		}
		present := make(map[string]bool, len(columns))
		for _, column := range columns {
			present[column.Name] = true
		}
		for _, column := range []struct {
			name       string
			definition string
		}{
			{name: "password_policy_version", definition: "INTEGER NOT NULL DEFAULT 0"},
			{name: "password_hash_version", definition: "INTEGER NOT NULL DEFAULT 0"},
			{name: "credential_generation", definition: "INTEGER NOT NULL DEFAULT 1"},
			{name: "mfa_generation", definition: "INTEGER NOT NULL DEFAULT 1"},
			{name: "password_changed_at", definition: "INTEGER NOT NULL DEFAULT 0"},
		} {
			if !present[column.name] {
				if err := tx.Exec("ALTER TABLE users ADD COLUMN " + column.name + " " + column.definition).Error; err != nil {
					return err
				}
			}
		}
	}
	for _, table := range []any{
		&model.AdminMFAFactor{},
		&model.AdminRecoveryCode{},
		&model.SecuritySession{},
		&model.StepUpGrant{},
	} {
		if !tx.Migrator().HasTable(table) {
			if err := tx.Migrator().CreateTable(table); err != nil {
				return err
			}
		}
	}
	if tx.Migrator().HasTable(&model.SecuritySession{}) &&
		!tx.Migrator().HasColumn(&model.SecuritySession{}, "LastMFAAt") {
		if err := tx.Migrator().AddColumn(&model.SecuritySession{}, "LastMFAAt"); err != nil {
			return err
		}
	}
	if tx.Migrator().HasTable(&model.Setting{}) {
		if err := tx.Exec(
			"INSERT INTO settings(key, value) SELECT ?, ? WHERE NOT EXISTS (SELECT 1 FROM settings WHERE key = ?)",
			settingcatalog.SessionLifetimePolicyKey,
			"legacy_unbounded",
			settingcatalog.SessionLifetimePolicyKey,
		).Error; err != nil {
			return err
		}
	}
	if err := tx.Model(&model.User{}).
		Where("credential_generation = 0 OR mfa_generation = 0").
		Updates(map[string]any{
			"credential_generation": gorm.Expr("CASE WHEN credential_generation = 0 THEN 1 ELSE credential_generation END"),
			"mfa_generation":        gorm.Expr("CASE WHEN mfa_generation = 0 THEN 1 ELSE mfa_generation END"),
		}).Error; err != nil {
		return err
	}
	// Legacy non-default credentials remain usable and expose policy version 0
	// as an explicit upgrade posture. Only bootstrap/default-credential
	// adaptation is allowed to set force_password_reset.
	return nil
}
