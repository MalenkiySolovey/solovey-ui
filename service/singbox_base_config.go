package service

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"gorm.io/gorm"
)

const defaultSingBoxBaseConfig = singboxconfig.DefaultBaseConfig

type SingBoxBaseConfigStore struct {
	settings *SettingService
}

func NewSingBoxBaseConfigStore(settings *SettingService) SingBoxBaseConfigStore {
	if settings == nil {
		settings = &SettingService{}
	}
	return SingBoxBaseConfigStore{settings: settings}
}

func (s SingBoxBaseConfigStore) Get() (string, error) {
	return s.settings.getString("config")
}

func (s SingBoxBaseConfigStore) Set(config string) error {
	configs, err := normalizeSingBoxBaseConfig(json.RawMessage(config))
	if err != nil {
		return err
	}
	return s.settings.setString("config", configs)
}

func (s SingBoxBaseConfigStore) Save(tx *gorm.DB, config json.RawMessage) error {
	configs, err := normalizeSingBoxBaseConfig(config)
	if err != nil {
		return err
	}
	result := tx.Model(model.Setting{}).Where("key = ?", "config").Update("value", configs)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return tx.Create(&model.Setting{Key: "config", Value: configs}).Error
	}
	return nil
}

func (s SingBoxBaseConfigStore) Changed(tx *gorm.DB, config json.RawMessage) (bool, error) {
	configs, err := normalizeSingBoxBaseConfig(config)
	if err != nil {
		return false, err
	}
	var stored model.Setting
	result := tx.Model(model.Setting{}).Where("key = ?", "config").Limit(1).Find(&stored)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return true, nil
	}
	return stored.Value != configs, nil
}

func normalizeSingBoxBaseConfig(config json.RawMessage) (string, error) {
	return singboxconfig.NormalizeBaseConfig(config)
}
