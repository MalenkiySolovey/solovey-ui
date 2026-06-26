package manager

import (
	settingscrypto "github.com/MalenkiySolovey/solovey-ui/internal/settings/crypto"
	settingsstore "github.com/MalenkiySolovey/solovey-ui/internal/settings/store"
	"gorm.io/gorm"
)

type ResealCodec interface {
	SecretboxCandidates() ([]settingscrypto.Candidate, error)
}

func ResealSecretSettings(db *gorm.DB, codec ResealCodec, encryptedKeys map[string]struct{}) (int, error) {
	return settingsstore.ResealSecretSettings(db, codec, encryptedKeys)
}
