package service

import (
	backendaccounts "github.com/MalenkiySolovey/solovey-backend/security/accounts"
	"gorm.io/gorm"
)

type ImportedPanelAccount = backendaccounts.ImportedPanelAccount

func (s *UserService) SaveImportedPanelAccount(tx *gorm.DB, command ImportedPanelAccount) error {
	return s.accounts().SaveImportedPanelAccount(tx, command)
}
