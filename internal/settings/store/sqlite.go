package store

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/gorm"
)

func Find(db *gorm.DB, key string) (*model.Setting, error) {
	setting := &model.Setting{}
	err := db.Model(model.Setting{}).Where("key = ?", key).First(setting).Error
	if err != nil {
		return nil, err
	}
	return setting, nil
}

func List(db *gorm.DB) ([]*model.Setting, error) {
	settings := make([]*model.Setting, 0)
	err := db.Model(model.Setting{}).Find(&settings).Error
	return settings, err
}

func EnsureDefaults(db *gorm.DB, keys []string, defaultValue func(key string) (string, bool)) error {
	var present int64
	if err := db.Model(model.Setting{}).Where("key IN ?", keys).Count(&present).Error; err != nil {
		return err
	}
	if int(present) == len(keys) {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, key := range keys {
			value, _ := defaultValue(key)
			if err := InsertIfMissing(tx, key, value); err != nil {
				return err
			}
		}
		return nil
	})
}

func DeleteAll(db *gorm.DB) error {
	return db.Where("1 = 1").Delete(model.Setting{}).Error
}

func InsertIfMissing(tx *gorm.DB, key string, value string) error {
	return tx.Exec(
		`INSERT INTO settings ("key", value)
		 SELECT ?, ?
		 WHERE NOT EXISTS (SELECT 1 FROM settings WHERE "key" = ?)`,
		key, value, key,
	).Error
}

func UpsertValue(tx *gorm.DB, key string, value string) error {
	result := tx.Model(model.Setting{}).Where("key = ?", key).Update("value", value)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return tx.Create(&model.Setting{Key: key, Value: value}).Error
	}
	return nil
}
