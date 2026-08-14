package enabledstate

import (
	"fmt"
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
	ids, err := EnabledIDs([]manifest.Manifest{item})
	if err != nil {
		return false, err
	}
	_, enabled := ids[item.ID]
	return enabled, nil
}

func EnabledIDs(available []manifest.Manifest) (map[string]struct{}, error) {
	ids := make(map[string]struct{}, len(available))
	defaults := make(map[string]bool, len(available))
	keys := make([]string, 0, len(available))
	for _, item := range available {
		if err := manifest.ValidateID(item.ID); err != nil {
			return nil, err
		}
		if _, duplicate := defaults[item.ID]; duplicate {
			return nil, fmt.Errorf("component %q is duplicated in enabled-state input", item.ID)
		}
		defaults[item.ID] = item.DefaultEnabled
		keys = append(keys, SettingKey(item.ID))
		if item.DefaultEnabled {
			ids[item.ID] = struct{}{}
		}
	}
	if len(keys) == 0 || dbsqlite.DB() == nil {
		return ids, nil
	}

	// Read the whole enabled set in one database snapshot. Per-component reads
	// both create an N+1 hot path and can combine values from different commits.
	var settings []model.Setting
	if err := dbsqlite.DB().Model(model.Setting{}).Where("key IN ?", keys).Find(&settings).Error; err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(settings))
	for _, setting := range settings {
		id, ok := ComponentIDFromSettingKey(setting.Key)
		if !ok {
			return nil, fmt.Errorf("enabled setting key %q is invalid", setting.Key)
		}
		if _, requested := defaults[id]; !requested {
			return nil, fmt.Errorf("enabled setting key %q was not requested", setting.Key)
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("component %q has duplicate enabled settings", id)
		}
		seen[id] = struct{}{}
		enabled, err := strconv.ParseBool(setting.Value)
		if err != nil {
			return nil, fmt.Errorf("component %q enabled setting is invalid: %w", id, err)
		}
		if enabled {
			ids[id] = struct{}{}
		} else {
			delete(ids, id)
		}
	}
	return ids, nil
}
