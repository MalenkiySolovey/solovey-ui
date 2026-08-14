package sqlite

import (
	"context"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	passwordutil "github.com/MalenkiySolovey/solovey-ui/util/password"

	"gorm.io/gorm"
)

const (
	legacyDefaultAdminUsername = "admin"
	legacyDefaultAdminPassword = "admin"
)

// rehashLegacyPasswords scans the users table and rewrites any password field
// that is not already an encoded password hash.
func rehashLegacyPasswords(tx *gorm.DB) error {
	var users []model.User
	if err := tx.Model(model.User{}).Find(&users).Error; err != nil {
		return err
	}
	for _, user := range users {
		if user.Password == "" {
			continue
		}
		if passwordutil.IsEncoded(user.Password) {
			continue
		}
		if isLegacyDefaultAdmin(user) {
			if err := rotateLegacyDefaultAdminPassword(tx, user); err != nil {
				return err
			}
			continue
		}
		hashed, err := passwordutil.Hash(context.Background(), user.Password)
		if err != nil {
			return err
		}
		if err := tx.Model(model.User{}).Where("id = ?", user.Id).Updates(map[string]any{
			"password":              hashed,
			"password_hash_version": passwordutil.PolicyVersion,
			"credential_generation": gorm.Expr("credential_generation + 1"),
		}).Error; err != nil {
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
	password, err := common.SecureRandom(24)
	if err != nil {
		return err
	}
	hashed, err := passwordutil.Hash(context.Background(), password)
	if err != nil {
		return err
	}
	passwordPath := initialAdminPasswordPath(configstorage.GetDBPath())
	if err := writeInitialAdminPassword(passwordPath, password); err != nil {
		return err
	}
	if err := tx.Model(model.User{}).Where("id = ?", user.Id).Updates(map[string]any{
		"password":                hashed,
		"force_password_reset":    true,
		"password_policy_version": passwordutil.PolicyVersion,
		"password_hash_version":   passwordutil.PolicyVersion,
		"credential_generation":   gorm.Expr("credential_generation + 1"),
	}).Error; err != nil {
		return err
	}
	notifyInitialAdminPasswordSaved(passwordPath)
	logger.Warningf("backup adapt: legacy admin/admin password rotated; new password saved to %s", passwordPath)
	return nil
}
