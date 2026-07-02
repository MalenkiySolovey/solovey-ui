package enabledstate

import (
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

const settingSuffix = ".enabled"

func SettingKey(id string) string {
	return id + settingSuffix
}

func ComponentIDFromSettingKey(key string) (string, bool) {
	id, ok := strings.CutSuffix(key, settingSuffix)
	if !ok || id == "" {
		return "", false
	}
	if err := manifest.ValidateID(id); err != nil {
		return "", false
	}
	return id, true
}

func IsSettingKey(key string) bool {
	_, ok := ComponentIDFromSettingKey(key)
	return ok
}

func ValidateSettingValue(value string) error {
	_, err := strconv.ParseBool(value)
	return err
}

func Enabled(item manifest.Manifest) (bool, error) {
	if err := manifest.ValidateID(item.ID); err != nil {
		return false, err
	}
	db := dbsqlite.DB()
	if db == nil {
		return item.DefaultEnabled, nil
	}
	var setting model.Setting
	err := db.Model(model.Setting{}).Where("key = ?", SettingKey(item.ID)).First(&setting).Error
	if dbsqlite.IsNotFound(err) {
		return item.DefaultEnabled, nil
	}
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(setting.Value)
}

func EnabledIDs(available []manifest.Manifest) (map[string]struct{}, error) {
	ids := make(map[string]struct{}, len(available))
	for _, item := range available {
		enabled, err := Enabled(item)
		if err != nil {
			return nil, err
		}
		if enabled {
			ids[item.ID] = struct{}{}
		}
	}
	return ids, nil
}
