package service

import (
	settingcatalog "github.com/MalenkiySolovey/solovey-backend/settings/catalog"
	"github.com/MalenkiySolovey/solovey-backend/settings/store"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation/catalogstore"
	"gorm.io/gorm"
)

type SettingService struct {
	// Only a temporary restore-local service supplies a context-bound DB.
	database *gorm.DB
}

func (s *SettingService) GetAllSetting() (*map[string]string, error) {
	settings, err := s.settingsManager().GetAll()
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

func (s *SettingService) ResetSettings() error {
	db := s.settingDatabase()
	if db == nil {
		return ErrMaintenanceUnavailable
	}
	return db.Transaction(func(tx *gorm.DB) error {
		local := &SettingService{database: tx}
		held, err := local.CoreMaintenance()
		if err != nil {
			return err
		}
		catalog, err := catalogstore.Legacy(tx)
		if err != nil {
			return err
		}
		// These markers describe the existing database, not resettable settings.
		var migrationVersions []model.Setting
		if err := tx.Select("key", "value").Where("key IN ?", []string{
			settingcatalog.VersionKey, settingcatalog.CoreSchemaVersionKey,
		}).Find(&migrationVersions).Error; err != nil {
			return err
		}
		if err := local.settingsManager().Reset(); err != nil {
			return err
		}
		for _, version := range migrationVersions {
			if err := store.UpsertValue(tx, version.Key, version.Value); err != nil {
				return err
			}
		}
		if catalog != nil {
			if err := store.UpsertValue(tx, zapretactivation.CatalogKey, string(catalog)); err != nil {
				return err
			}
		}
		value := "false"
		if held {
			value = "true"
		}
		if err := local.setCoreMaintenanceValue(value); err != nil {
			return err
		}
		// Reset retains deliberate downtime and adopts the bounded session policy.
		return local.AdoptBoundedSessionLifetime()
	})
}

func (s *SettingService) GetComponentSettingString(key string) (string, error) {
	return s.getString(key)
}

func (s *SettingService) SetComponentSettingString(key string, value string) error {
	return s.setString(key, value)
}

func (s *SettingService) GetComponentSettingBool(key string) (bool, error) {
	return s.getBool(key)
}

func (s *SettingService) GetComponentSettingInt(key string) (int, error) {
	return s.getInt(key)
}

func (s *SettingService) GetComponentSettingSecretBytes(key string) ([]byte, error) {
	setting, err := s.getSetting(key)
	if settingNotFound(err) {
		value, _ := defaultSettingValue(key)
		return []byte(value), nil
	}
	if err != nil {
		return nil, err
	}
	return s.decryptSettingBytes(key, setting.Value)
}

func (s *SettingService) HasComponentSettingSecret(key string) (bool, error) {
	setting, err := s.getSetting(key)
	if settingNotFound(err) {
		value, _ := defaultSettingValue(key)
		return value != "", nil
	}
	if err != nil {
		return false, err
	}
	return setting.Value != "", nil
}
