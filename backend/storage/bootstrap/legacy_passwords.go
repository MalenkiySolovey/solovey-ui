package bootstrap

import (
	"context"

	backendcrypto "github.com/MalenkiySolovey/solovey-ui/backend/security/crypto"
	passwordutil "github.com/MalenkiySolovey/solovey-ui/backend/security/password"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"

	"gorm.io/gorm"
)

const (
	legacyDefaultAdminUsername = "admin"
	legacyDefaultAdminPassword = "admin"
)

// RehashLegacyPasswords scans the users table and rewrites any password field
// that is not already an encoded password hash.
func (i Initializer) RehashLegacyPasswords(tx *gorm.DB) error {
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
			if err := i.rotateLegacyDefaultAdminPassword(tx, user); err != nil {
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
		i.infof("backup adapt: rehashed plaintext password for user %q", user.Username)
	}
	return nil
}

func isLegacyDefaultAdmin(user model.User) bool {
	return user.Username == legacyDefaultAdminUsername && user.Password == legacyDefaultAdminPassword
}

func (i Initializer) rotateLegacyDefaultAdminPassword(tx *gorm.DB, user model.User) error {
	password, err := backendcrypto.SecureRandom(24)
	if err != nil {
		return err
	}
	hashed, err := passwordutil.Hash(context.Background(), password)
	if err != nil {
		return err
	}
	passwordPath := i.InitialAdminPasswordPath(i.DatabasePath)
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
	i.warningf("backup adapt: legacy admin/admin password rotated; new password saved to %s", passwordPath)
	return nil
}
