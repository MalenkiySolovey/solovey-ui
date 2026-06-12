package service

import (
	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/gorm"
)

type SettingService struct {
}

func (s *SettingService) GetAllSetting() (*map[string]string, error) {
	db := database.GetDB()
	if err := s.ensureDefaultSettings(db); err != nil {
		return nil, err
	}

	settings := make([]*model.Setting, 0)
	err := db.Model(model.Setting{}).Find(&settings).Error
	if err != nil {
		return nil, err
	}
	allSetting := map[string]string{}

	for _, setting := range settings {
		if isEncryptedSettingKey(setting.Key) {
			writeSecretSettingMarker(allSetting, setting.Key, setting.Value)
			continue
		}
		allSetting[setting.Key] = setting.Value
	}

	hideInternalSettings(allSetting)

	return &allSetting, nil
}

func (s *SettingService) ensureDefaultSettings(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, key := range defaultSettingKeys() {
			value, _ := defaultSettingValue(key)
			if err := insertSettingIfMissing(tx, key, value); err != nil {
				return err
			}
		}
		return nil
	})
}

func insertSettingIfMissing(tx *gorm.DB, key string, value string) error {
	return tx.Exec(
		`INSERT INTO settings ("key", value)
		 SELECT ?, ?
		 WHERE NOT EXISTS (SELECT 1 FROM settings WHERE "key" = ?)`,
		key, value, key,
	).Error
}

func (s *SettingService) ResetSettings() error {
	db := database.GetDB()
	return db.Where("1 = 1").Delete(model.Setting{}).Error
}
