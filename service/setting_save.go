package service

import (
	"encoding/json"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/gorm"
)

func (s *SettingService) Save(tx *gorm.DB, data json.RawMessage) error {
	settings, err := decodeSettingSaveData(data)
	if err != nil {
		return err
	}
	if err = s.validateSaveKeys(settings); err != nil {
		return err
	}
	if err = s.validateAll(settings); err != nil {
		return err
	}
	for _, key := range sortedSettingKeys(settings) {
		value, shouldSave, err := s.prepareSettingSaveValue(key, settings[key])
		if err != nil {
			return err
		}
		if !shouldSave {
			continue
		}
		if err = applySettingSaveSideEffects(tx, key, value); err != nil {
			return err
		}
		if err = upsertSettingValue(tx, key, value); err != nil {
			return err
		}
	}
	return nil
}

func decodeSettingSaveData(data json.RawMessage) (map[string]string, error) {
	var settings map[string]string
	err := json.Unmarshal(data, &settings)
	return settings, err
}

func (s *SettingService) prepareSettingSaveValue(key string, value string) (string, bool, error) {
	if strings.HasSuffix(key, "HasSecret") {
		return "", false, nil
	}
	value, shouldSave, err := s.prepareEncryptedSettingSaveValue(key, value)
	if err != nil || !shouldSave {
		return value, shouldSave, err
	}
	if isPathSetting(key) {
		value, err = normalizeAndValidatePathSetting(key, value)
		if err != nil {
			return "", false, err
		}
	}
	return value, true, nil
}

func (s *SettingService) prepareEncryptedSettingSaveValue(key string, value string) (string, bool, error) {
	if !isEncryptedSettingKey(key) {
		return value, true, nil
	}
	if value == StoredSecretMarker {
		return "", false, nil
	}
	if value == "" {
		return "", key == settingKeyTelegramBackupPassphrase, nil
	}
	encrypted, err := s.encryptSettingValue(key, value)
	if err != nil {
		return "", false, err
	}
	return encrypted, true, nil
}

func applySettingSaveSideEffects(tx *gorm.DB, key string, value string) error {
	if key != "trafficAge" || value != "0" {
		return nil
	}
	return tx.Where("id > 0").Delete(model.Stats{}).Error
}

func upsertSettingValue(tx *gorm.DB, key string, value string) error {
	result := tx.Model(model.Setting{}).Where("key = ?", key).Update("value", value)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return tx.Create(&model.Setting{Key: key, Value: value}).Error
	}
	return nil
}
