package service

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	settingsmanager "github.com/MalenkiySolovey/solovey-ui/internal/settings/manager"
	subformats "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/formats"

	"gorm.io/gorm"
)

func (s *SettingService) Save(tx *gorm.DB, data json.RawMessage) error {
	settings, err := settingsmanager.DecodeSaveData(data)
	if err != nil {
		return err
	}
	if raw, present := settings[settingKeySubJsonExt]; present {
		prepared, err := subformats.CanonicalJSONExtension([]byte(raw))
		if err != nil {
			return err
		}
		settings[settingKeySubJsonExt] = string(prepared.Candidate)
		data, err = json.Marshal(settings)
		if err != nil {
			return err
		}
	}
	return s.settingsManager().Save(tx, data)
}

func applySettingSaveSideEffects(tx *gorm.DB, key string, value string) error {
	if key != "trafficAge" || value != "0" {
		return nil
	}
	return tx.Where("id > 0").Delete(model.Stats{}).Error
}
