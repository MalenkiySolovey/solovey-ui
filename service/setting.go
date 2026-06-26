package service

type SettingService struct {
}

func (s *SettingService) GetAllSetting() (*map[string]string, error) {
	settings, err := s.settingsManager().GetAll()
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

func (s *SettingService) ResetSettings() error {
	return s.settingsManager().Reset()
}
