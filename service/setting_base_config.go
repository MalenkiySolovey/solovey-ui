package service

import (
	"encoding/json"

	"gorm.io/gorm"
)

func (s *SettingService) GetConfig() (string, error) {
	return NewSingBoxBaseConfigStore(s).Get()
}

func (s *SettingService) SetConfig(config string) error {
	return NewSingBoxBaseConfigStore(s).Set(config)
}

func (s *SettingService) SaveConfig(tx *gorm.DB, config json.RawMessage) error {
	return NewSingBoxBaseConfigStore(s).Save(tx, config)
}

func (s *SettingService) ConfigBlobChanged(tx *gorm.DB, config json.RawMessage) (bool, error) {
	return NewSingBoxBaseConfigStore(s).Changed(tx, config)
}
