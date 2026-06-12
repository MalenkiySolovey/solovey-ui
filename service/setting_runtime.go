package service

import (
	"time"

	"github.com/MalenkiySolovey/solovey-ui/logger"
)

func (s *SettingService) GetTrafficAge() (int, error) {
	return s.getInt("trafficAge")
}

func (s *SettingService) GetAuditRetentionDays() (int, error) {
	return s.getInt("auditRetentionDays")
}

func (s *SettingService) GetIPHistoryRetentionDays() (int, error) {
	return s.getInt("ipHistoryRetentionDays")
}

func (s *SettingService) GetIPShowRaw() (bool, error) {
	return s.getBool("ipShowRaw")
}

func (s *SettingService) GetObservabilityMemoryCapMB() (int, error) {
	return s.getInt("observabilityMemoryCapMB")
}

func (s *SettingService) GetTimeLocation() (*time.Location, error) {
	l, err := s.getString("timeLocation")
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
