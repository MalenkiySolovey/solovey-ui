package service

import (
	"time"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

func (s *SettingService) GetTrafficAge() (int, error) {
	return s.getInt(settingcatalog.TrafficAgeKey)
}

func (s *SettingService) GetAuditRetentionDays() (int, error) {
	return s.getInt(settingcatalog.AuditRetentionDaysKey)
}

func (s *SettingService) GetIPHistoryRetentionDays() (int, error) {
	return s.getInt(settingcatalog.IPHistoryRetentionDaysKey)
}

func (s *SettingService) GetIPShowRaw() (bool, error) {
	return s.getBool(settingcatalog.IPShowRawKey)
}

func (s *SettingService) GetObservabilityMemoryCapMB() (int, error) {
	return s.getInt(settingcatalog.ObservabilityMemoryCapMBKey)
}

func (s *SettingService) GetTimeLocation() (*time.Location, error) {
	l, err := s.getString(settingcatalog.TimeLocationKey)
	if err != nil {
		return nil, err
	}
	location, err := time.LoadLocation(l)
	if err != nil {
		logger.Warningf("location <%v> not exist, using local location", l)
		return time.Local, nil
	}
	return location, nil
}
