package sqlite

import (
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

const (
	legacyDefaultAdminUsername = "admin"
	legacyDefaultAdminPassword = "admin"
)

// rehashLegacyPasswords scans the users table and rewrites any password field
// that is not already a bcrypt hash. The wire format from `common.HashPassword`
// stores the bcrypt blob behind a `bcrypt:` prefix; raw `$2[aby]$...` blobs
// (which old backups never had, but might appear via manual edits) are also
// considered hashed.
func rehashLegacyPasswords(tx *gorm.DB) error {
	var users []model.User
	if err := tx.Model(model.User{}).Find(&users).Error; err != nil {
		return err
	}
	for _, user := range users {
		if user.Password == "" {
			continue
		}
		if common.IsPasswordHash(user.Password) {
			continue
		}
		if isLegacyDefaultAdmin(user) {
			if err := rotateLegacyDefaultAdminPassword(tx, user); err != nil {
				return err
			}
			continue
		}
		hashed, err := common.HashPassword(user.Password)
		if err != nil {
			return err
		}
		if err := tx.Model(model.User{}).Where("id = ?", user.Id).Update("password", hashed).Error; err != nil {
			return err
		}
		logger.Infof("backup adapt: rehashed plaintext password for user %q", user.Username)
	}
	return nil
}

func isLegacyDefaultAdmin(user model.User) bool {
	return user.Username == legacyDefaultAdminUsername && user.Password == legacyDefaultAdminPassword
}

func rotateLegacyDefaultAdminPassword(tx *gorm.DB, user model.User) error {
	password := common.Random(24)
	hashed, err := common.HashPassword(password)
	if err != nil {
		return err
	}
	passwordPath := initialAdminPasswordPath(configstorage.GetDBPath())
	if err := writeInitialAdminPassword(passwordPath, password); err != nil {
		return err
	}
	if err := tx.Model(model.User{}).Where("id = ?", user.Id).Updates(map[string]any{
		"password":             hashed,
		"force_password_reset": false,
	}).Error; err != nil {
		return err
	}
	notifyInitialAdminPasswordSaved(passwordPath)
	logger.Warningf("backup adapt: legacy admin/admin password rotated; new password saved to %s", passwordPath)
	return nil
}
