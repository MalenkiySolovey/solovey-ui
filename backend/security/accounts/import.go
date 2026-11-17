package accounts

import (
	"errors"
	"time"

	passwordutil "github.com/MalenkiySolovey/solovey-ui/backend/security/password"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"gorm.io/gorm"
)

// ImportedPanelAccount is a command issued only after import plan approval.
// KeepExistingPassword implements the current reset-required merge choice.
type ImportedPanelAccount struct {
	Username             string
	PasswordHash         string
	ForcePasswordReset   bool
	KeepExistingPassword bool
}

func (s *Service) SaveImportedPanelAccount(tx *gorm.DB, command ImportedPanelAccount) error {
	if tx == nil {
		return errors.New("panel account persistence is unavailable")
	}
	if s == nil || s.ValidateImportName == nil {
		return errors.New("administrator import validation is unavailable")
	}
	if err := s.ValidateImportName(command.Username); err != nil {
		return err
	}
	if !passwordutil.IsEncoded(command.PasswordHash) {
		return errors.New("panel account import requires a supported password hash")
	}
	var user model.User
	err := tx.Where("username = ?", command.Username).First(&user).Error
	if err != nil && !dbsqlite.IsNotFound(err) {
		return err
	}
	if dbsqlite.IsNotFound(err) {
		order, err := s.nextSortOrder(tx)
		if err != nil {
			return err
		}
		return tx.Create(&model.User{SortOrder: order, Username: command.Username,
			Password: command.PasswordHash, ForcePasswordReset: command.ForcePasswordReset,
			CredentialGeneration: 1, MFAGeneration: 1, PasswordChangedAt: time.Now().Unix()}).Error
	}
	updates := map[string]any{"force_password_reset": command.ForcePasswordReset,
		"credential_generation": NormalizeGeneration(user.CredentialGeneration) + 1}
	if !command.KeepExistingPassword {
		updates["password"] = command.PasswordHash
		updates["password_changed_at"] = time.Now().Unix()
		updates["password_hash_version"] = 0 // authentication upgrades compatible foreign hashes
	}
	// Preserve MFA, enrollment, token and policy ownership. Generation changes
	// invalidate existing assurance even when the imported password is equal.
	return tx.Model(&model.User{}).Where("id = ?", user.Id).Updates(updates).Error
}
