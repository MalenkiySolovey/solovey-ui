package service

import (
	"context"
	"errors"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
)

var ErrMaintenanceUnavailable = errors.New("maintenance state unavailable")

func (s *SettingService) setCoreMaintenanceValue(value string) error {
	return s.setString(settingcatalog.CoreMaintenanceKey, value)
}

// Settings owns the sole durable fact. Missing old keys default off; malformed
// values and read errors never grant lifecycle permission to start a core.
func (s *SettingService) CoreMaintenance() (bool, error) {
	value, err := s.getString(settingcatalog.CoreMaintenanceKey)
	if err != nil || value != "true" && value != "false" {
		return false, ErrMaintenanceUnavailable
	}
	return value == "true", nil
}

// CoreMaintenanceContext preserves an existing database maintenance admission
// for health/recovery reads without granting a fresh ordinary operation lease.
func (s *SettingService) CoreMaintenanceContext(ctx context.Context) (bool, error) {
	db := s.settingDatabase()
	if ctx == nil || db == nil {
		return false, ErrMaintenanceUnavailable
	}
	settings := SettingService{database: db.WithContext(ctx)}
	return settings.CoreMaintenance()
}
