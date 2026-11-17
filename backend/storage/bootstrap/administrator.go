package bootstrap

import (
	"os"

	backendcrypto "github.com/MalenkiySolovey/solovey-ui/backend/security/crypto"
	passwordutil "github.com/MalenkiySolovey/solovey-ui/backend/security/password"
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/backend/settings/catalog"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"gorm.io/gorm"
)

func (i Initializer) EnsureInitialAdmin(database *gorm.DB, dbPath string) error {
	var count int64
	if err := database.Model(&model.User{}).Count(&count).Error; err != nil {
		return err
	}
	passwordPath := i.InitialAdminPasswordPath(dbPath)
	if count != 0 {
		i.warnIfInitialAdminPasswordFileExists(passwordPath)
		return nil
	}

	password, err := backendcrypto.SecureRandom(24)
	if err != nil {
		return err
	}
	passwordHash, err := passwordutil.Hash(database.Statement.Context, password)
	if err != nil {
		return err
	}
	if err := writeInitialAdminPassword(passwordPath, password); err != nil {
		return err
	}
	if err := database.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&model.User{
			Username:              "admin",
			Password:              passwordHash,
			ForcePasswordReset:    true,
			PasswordPolicyVersion: passwordutil.PolicyVersion,
			PasswordHashVersion:   passwordutil.PolicyVersion,
			CredentialGeneration:  1,
			MFAGeneration:         1,
		}).Error; err != nil {
			return err
		}
		// Fresh installations start with bounded sessions. Existing installations
		// deliberately receive legacy_unbounded from the 1.8 migration/default
		// fallback and require an explicit operator adoption.
		return tx.Exec(
			"INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
			settingcatalog.SessionLifetimePolicyKey,
			"bounded_v1",
		).Error
	}); err != nil {
		_ = os.Remove(passwordPath)
		return err
	}
	notifyInitialAdminPasswordSaved(passwordPath)
	return nil
}
