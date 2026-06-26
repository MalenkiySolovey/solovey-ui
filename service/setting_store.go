package service

import (
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"gorm.io/gorm"
)

func settingsDatabase() *gorm.DB {
	return dbsqlite.DB()
}

func settingNotFound(err error) bool {
	return dbsqlite.IsNotFound(err)
}

func settingsDatabaseAvailable() bool {
	return settingsDatabase() != nil
}

func (s *SettingService) getSetting(key string) (*model.Setting, error) {
	return s.settingsManager().Find(key)
}

func (s *SettingService) getString(key string) (string, error) {
	return s.settingsManager(true).GetString(key)
}

func (s *SettingService) saveSetting(key string, value string) error {
	return s.settingsManager().SetString(key, value)
}

func (s *SettingService) setString(key string, value string) error {
	return s.saveSetting(key, value)
}

func (s *SettingService) setEncryptedString(key string, value string) error {
	return s.settingsManager().SetEncryptedString(key, value)
}

func (s *SettingService) getBool(key string) (bool, error) {
	str, err := s.getString(key)
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(str)
}

// func (s *SettingService) setBool(key string, value bool) error {
// 	return s.setString(key, strconv.FormatBool(value))
// }

func (s *SettingService) getInt(key string) (int, error) {
	str, err := s.getString(key)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(str)
}

func (s *SettingService) setInt(key string, value int) error {
	return s.setString(key, strconv.Itoa(value))
}
