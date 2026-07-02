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

func (s *SettingService) GetComponentSettingString(key string) (string, error) {
	return s.getString(key)
}

func (s *SettingService) SetComponentSettingString(key string, value string) error {
	return s.setString(key, value)
}

func (s *SettingService) GetComponentSettingBool(key string) (bool, error) {
	return s.getBool(key)
}

func (s *SettingService) GetComponentSettingInt(key string) (int, error) {
	return s.getInt(key)
}

func (s *SettingService) GetComponentSettingSecretBytes(key string) ([]byte, error) {
	setting, err := s.getSetting(key)
	if settingNotFound(err) {
		value, _ := defaultSettingValue(key)
		return []byte(value), nil
	}
	if err != nil {
		return nil, err
	}
	return s.decryptSettingBytes(key, setting.Value)
}

func (s *SettingService) HasComponentSettingSecret(key string) (bool, error) {
	setting, err := s.getSetting(key)
	if settingNotFound(err) {
		value, _ := defaultSettingValue(key)
		return value != "", nil
	}
	if err != nil {
		return false, err
	}
	return setting.Value != "", nil
}
