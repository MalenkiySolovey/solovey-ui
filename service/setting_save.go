package service

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/gorm"
)

func (s *SettingService) Save(tx *gorm.DB, data json.RawMessage) error {
	return s.settingsManager().Save(tx, data)
}

func applySettingSaveSideEffects(tx *gorm.DB, key string, value string) error {
	if key != "trafficAge" || value != "0" {
		return nil
	}
	return tx.Where("id > 0").Delete(model.Stats{}).Error
}
