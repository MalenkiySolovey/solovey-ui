package service

import (
	"encoding/json"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	singboxvalidation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"
	"github.com/MalenkiySolovey/solovey-ui/logger"
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
	db := s.settings.settingDatabase()
	if db == nil {
		return errors.New("configuration database is unavailable")
	}
	return db.Transaction(func(tx *gorm.DB) error { return s.Save(tx, json.RawMessage(config)) })
}

func (s SingBoxBaseConfigStore) Save(tx *gorm.DB, config json.RawMessage) error {
	if tx == nil {
		return errors.New("configuration database is unavailable")
	}
	return tx.Transaction(func(candidate *gorm.DB) error { return s.save(candidate, config) })
}

func (s SingBoxBaseConfigStore) save(tx *gorm.DB, config json.RawMessage) error {
	var submitted map[string]json.RawMessage
	_ = json.Unmarshal(config, &submitted)
	if _, hasProviders := submitted["certificate_providers"]; hasProviders {
		projection, err := NewSingBoxConfigBuilder(nil).BuildCandidateProjectionFromDB(tx, string(config), false)
		if err != nil {
			return err
		}
		if err := singboxvalidation.ValidateConfig(projection.Config); err != nil {
			return err
		}
	}
	definitions, err := entitytls.ReadProviderDefinitions(tx)
	if err != nil {
		return err
	}
	base, definitions, _, err := entitytls.PrepareBaseProviders(config, definitions)
	if err != nil {
		return err
	}
	config = base
	configs, err := normalizeSingBoxBaseConfig(config)
	if err != nil {
		return err
	}
	if err := entitytls.WriteProviderDefinitions(tx, definitions); err != nil {
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
	var root map[string]json.RawMessage
	if json.Unmarshal(config, &root) == nil {
		if _, providers := root["certificate_providers"]; providers {
			return true, nil
		}
	}
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
	var root map[string]json.RawMessage
	if json.Unmarshal(config, &root) == nil {
		if _, present := root["certificate_providers"]; present {
			return "", errors.New("TLS_PROVIDER_STORE_REQUIRED: submit certificate providers through the TLS-owned config save adapter")
		}
	}
	findings, err := singboxvalidation.ValidateRuleConditions(config)
	if err != nil {
		return "", err
	}
	for _, finding := range findings {
		logger.Warningf("config rule validation: %s [%s]: %s", finding.Path, finding.Code, finding.Message)
	}
	canonical, err := singboxconfig.CanonicalDNSConfig(config)
	if err != nil {
		return "", err
	}
	if _, err := singboxconfig.ValidateHTTPConfig(canonical); err != nil {
		return "", err
	}
	return singboxconfig.NormalizeBaseConfig(canonical)
}
