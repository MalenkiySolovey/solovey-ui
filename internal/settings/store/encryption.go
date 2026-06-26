package store

import (
	"os"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	settingscrypto "github.com/MalenkiySolovey/solovey-ui/internal/settings/crypto"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/gorm"
)

type ResealCodec interface {
	SecretboxCandidates() ([]settingscrypto.Candidate, error)
}

func ResealSecretSettings(db *gorm.DB, codec ResealCodec, encryptedKeys map[string]struct{}) (int, error) {
	if strings.TrimSpace(os.Getenv("SUI_SECRETBOX_KEY")) == "" {
		return 0, nil
	}
	if db == nil {
		return 0, nil
	}
	candidates, err := codec.SecretboxCandidates()
	if err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	resealed := 0
	for key := range encryptedKeys {
		var setting model.Setting
		if err := db.Model(model.Setting{}).Where("key = ?", key).First(&setting).Error; err != nil {
			if !dbsqlite.IsNotFound(err) {
				logger.Warning("reseal secret setting: read failed for", key, ":", err)
			}
			continue
		}
		if setting.Value == "" || !secretbox.IsEncrypted(setting.Value) {
			continue
		}
		idx, plaintext, ok := settingscrypto.DecryptWithCandidate(candidates, key, setting.Value)
		if !ok {
			logger.Warning("reseal secret setting: no candidate decrypts", key, "; leaving as-is")
			continue
		}
		if idx == 0 {
			continue
		}
		sealed, err := candidates[0].Box.EncryptString(plaintext, key)
		if err != nil {
			logger.Warning("reseal secret setting: re-encrypt failed for", key, ":", err)
			continue
		}
		if err := db.Model(model.Setting{}).Where("key = ?", key).Update("value", sealed).Error; err != nil {
			logger.Warning("reseal secret setting: persist failed for", key, ":", err)
			continue
		}
		resealed++
	}
	return resealed, nil
}
