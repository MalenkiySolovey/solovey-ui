package service

import (
	"strconv"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"gorm.io/gorm"
)

func configDatabase() *gorm.DB {
	return dbsqlite.DB()
}

func (s *ConfigService) CheckChanges(lastSeen string) (bool, error) {
	if lastSeen == "" {
		return true, nil
	}
	lastSeenAt, err := strconv.ParseInt(lastSeen, 10, 64)
	if err != nil {
		return false, err
	}
	lastUpdate := s.getLastUpdate()
	if lastUpdate != 0 {
		return lastUpdate > lastSeenAt, nil
	}

	var count int64
	err = configDatabase().Model(model.Changes{}).Where("date_time > ?", lastSeenAt).Count(&count).Error
	if err == nil {
		s.setLastUpdate(time.Now().Unix())
	}
	return count > 0, err
}

func (s *ConfigService) GetChanges(actor string, changeKey string, count string) []model.Changes {
	limit, _ := strconv.Atoi(count)
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	db := configDatabase().Model(model.Changes{})
	if actor != "" {
		db = db.Where("actor = ?", actor)
	}
	if changeKey != "" {
		db = db.Where("key = ?", changeKey)
	}
	var changes []model.Changes
	if err := db.Order("id desc").Limit(limit).Scan(&changes).Error; err != nil {
		logger.Warning(err)
	}
	return changes
}
