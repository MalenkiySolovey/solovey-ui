package manager

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	settingsschema "github.com/MalenkiySolovey/solovey-ui/internal/settings/schema"
	settingsstore "github.com/MalenkiySolovey/solovey-ui/internal/settings/store"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

type SecretCodec interface {
	EncryptString(key string, value string) (string, error)
	DecryptString(key string, value string) (string, error)
	WriteMarker(settings map[string]string, key string, value string)
}

type Hooks struct {
	ValidateAll            func(settings map[string]string) error
	NormalizePath          func(key string, value string) (string, bool, error)
	ApplySideEffects       func(tx *gorm.DB, key string, value string) error
	CanClearEmptyEncrypted func(key string) bool
}

type Manager struct {
	DB           func() *gorm.DB
	Schema       settingsschema.Schema
	DefaultValue func(key string) (string, bool)
	Secret       SecretCodec
	StoredSecret string
	Hooks        Hooks
}

func (m Manager) GetAll() (map[string]string, error) {
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	if err := m.EnsureDefaults(db); err != nil {
		return nil, err
	}
	settings, err := settingsstore.List(db)
	if err != nil {
		return nil, err
	}
	allSetting := map[string]string{}
	for _, setting := range settings {
		if m.Schema.Encrypted(setting.Key) {
			if m.Secret != nil {
				m.Secret.WriteMarker(allSetting, setting.Key, setting.Value)
			}
			continue
		}
		allSetting[setting.Key] = setting.Value
	}
	m.Schema.HideInternal(allSetting)
	return allSetting, nil
}

func (m Manager) EnsureDefaults(db *gorm.DB) error {
	return settingsstore.EnsureDefaults(db, m.Schema.Keys(), m.defaultValue)
}

func (m Manager) Reset() error {
	db, err := m.db()
	if err != nil {
		return err
	}
	return settingsstore.DeleteAll(db)
}

func (m Manager) Find(key string) (*model.Setting, error) {
	db, err := m.db()
	if err != nil {
		return nil, err
	}
	return settingsstore.Find(db, key)
}

func (m Manager) GetString(key string) (string, error) {
	setting, err := m.Find(key)
	if dbsqlite.IsNotFound(err) {
		value, ok := m.defaultValue(key)
		if !ok {
			return "", common.NewErrorf("key <%v> not in defaultValueMap", key)
		}
		return value, nil
	} else if err != nil {
		return "", err
	}
	if m.Schema.Encrypted(key) {
		if m.Secret == nil {
			return "", common.NewError("settings secret codec is not configured")
		}
		return m.Secret.DecryptString(key, setting.Value)
	}
	return setting.Value, nil
}

func (m Manager) SetString(key string, value string) error {
	db, err := m.db()
	if err != nil {
		return err
	}
	return settingsstore.UpsertValue(db, key, value)
}

func (m Manager) SetEncryptedString(key string, value string) error {
	if m.Secret == nil {
		return common.NewError("settings secret codec is not configured")
	}
	encrypted, err := m.Secret.EncryptString(key, value)
	if err != nil {
		return err
	}
	return m.SetString(key, encrypted)
}

func (m Manager) Save(tx *gorm.DB, data json.RawMessage) error {
	settings, err := DecodeSaveData(data)
	if err != nil {
		return err
	}
	if err = m.validateSaveKeys(settings); err != nil {
		return err
	}
	if m.Hooks.ValidateAll != nil {
		if err = m.Hooks.ValidateAll(settings); err != nil {
			return err
		}
	}
	for _, key := range settingcatalog.SortedKeys(settings) {
		value, shouldSave, err := m.PrepareSaveValue(key, settings[key])
		if err != nil {
			return err
		}
		if !shouldSave {
			continue
		}
		if m.Hooks.ApplySideEffects != nil {
			if err = m.Hooks.ApplySideEffects(tx, key, value); err != nil {
				return err
			}
		}
		if err = settingsstore.UpsertValue(tx, key, value); err != nil {
			return err
		}
	}
	return nil
}

func DecodeSaveData(data json.RawMessage) (map[string]string, error) {
	var settings map[string]string
	err := json.Unmarshal(data, &settings)
	return settings, err
}

func (m Manager) PrepareSaveValue(key string, value string) (string, bool, error) {
	if settingsschema.IsSecretPresenceMarker(key) {
		return "", false, nil
	}
	value, shouldSave, err := m.PrepareEncryptedSaveValue(key, value)
	if err != nil || !shouldSave {
		return value, shouldSave, err
	}
	if m.Hooks.NormalizePath != nil {
		var normalized bool
		value, normalized, err = m.Hooks.NormalizePath(key, value)
		if err != nil {
			return "", false, err
		}
		if normalized {
			return value, true, nil
		}
	}
	return value, true, nil
}

func (m Manager) validateSaveKeys(settings map[string]string) error {
	for _, key := range settingcatalog.SortedKeys(settings) {
		if settingsschema.IsSecretPresenceMarker(key) {
			if m.Schema.AcceptsSecretPresenceMarker(key) {
				continue
			}
			return common.NewError("invalid setting key: ", key)
		}
		if !m.Schema.Editable(key) {
			return common.NewError("invalid setting key: ", key)
		}
	}
	return nil
}

func (m Manager) PrepareEncryptedSaveValue(key string, value string) (string, bool, error) {
	if !m.Schema.Encrypted(key) {
		return value, true, nil
	}
	if value == m.StoredSecret {
		return "", false, nil
	}
	if value == "" {
		if m.Hooks.CanClearEmptyEncrypted == nil {
			return "", false, nil
		}
		return "", m.Hooks.CanClearEmptyEncrypted(key), nil
	}
	if m.Secret == nil {
		return "", false, common.NewError("settings secret codec is not configured")
	}
	encrypted, err := m.Secret.EncryptString(key, value)
	if err != nil {
		return "", false, err
	}
	return encrypted, true, nil
}

func (m Manager) db() (*gorm.DB, error) {
	if m.DB == nil {
		return nil, common.NewError("settings database provider is not configured")
	}
	db := m.DB()
	if db == nil {
		return nil, common.NewError("database is not initialized")
	}
	return db, nil
}

func (m Manager) defaultValue(key string) (string, bool) {
	if m.DefaultValue != nil {
		return m.DefaultValue(key)
	}
	return m.Schema.Default(key)
}
