package store

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	settingscrypto "github.com/MalenkiySolovey/solovey-ui/internal/settings/crypto"
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
		return 0, errors.New("settings persistence is unavailable")
	}
	if codec == nil {
		return 0, errors.New("settings reseal codec is unavailable")
	}
	candidates, err := codec.SecretboxCandidates()
	if err != nil {
		return 0, err
	}
	if len(candidates) == 0 {
		return 0, errors.New("settings reseal codec returned no key candidates")
	}
	for _, candidate := range candidates {
		if candidate.Box == nil {
			return 0, errors.New("settings reseal codec returned an invalid key candidate")
		}
	}
	keys := make([]string, 0, len(encryptedKeys))
	for key := range encryptedKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	resealed := 0
	err = db.Transaction(func(tx *gorm.DB) error {
		for _, key := range keys {
			var setting model.Setting
			if err := tx.Model(model.Setting{}).Where("key = ?", key).First(&setting).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return fmt.Errorf("read secret setting %s for reseal: %w", key, err)
			}
			if setting.Value == "" || !secretbox.IsEncrypted(setting.Value) {
				continue
			}
			idx, plaintext, ok := settingscrypto.DecryptWithCandidate(candidates, key, setting.Value)
			if !ok {
				return fmt.Errorf("decrypt secret setting %s for reseal", key)
			}
			if idx == 0 {
				continue
			}
			sealed, err := candidates[0].Box.EncryptString(plaintext, key)
			if err != nil {
				return fmt.Errorf("encrypt secret setting %s for reseal: %w", key, err)
			}
			result := tx.Model(model.Setting{}).Where("id = ?", setting.Id).Update("value", sealed)
			if result.Error != nil {
				return fmt.Errorf("persist resealed secret setting %s: %w", key, result.Error)
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("persist resealed secret setting %s: row changed concurrently", key)
			}
			resealed++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return resealed, nil
}
