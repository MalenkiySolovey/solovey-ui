package service

func (s *SettingService) GetIpCertEnabled() (bool, error) {
	return s.getBool(settingKeyIpCertEnabled)
}

func (s *SettingService) SetIpCertEnabled(v bool) error {
	value := "false"
	if v {
		value = "true"
	}
	return s.setString(settingKeyIpCertEnabled, value)
}

func (s *SettingService) GetIpCertTargetIP() (string, error) {
	return s.getString(settingKeyIpCertTargetIP)
}

func (s *SettingService) GetIpCertEmail() (string, error) {
	return s.getString(settingKeyIpCertEmail)
}

func (s *SettingService) GetIpCertChallengePort() (int, error) {
	return s.getInt(settingKeyIpCertChallengePort)
}

func (s *SettingService) GetIpCertApplyTarget() (string, error) {
	return s.getString(settingKeyIpCertApplyTarget)
}

func (s *SettingService) setIpCertTargetIP(v string) error {
	return s.setString(settingKeyIpCertTargetIP, v)
}

func (s *SettingService) setIpCertEmail(v string) error {
	return s.setString(settingKeyIpCertEmail, v)
}

func (s *SettingService) setIpCertChallengePort(v int) error {
	return s.setInt(settingKeyIpCertChallengePort, v)
}

func (s *SettingService) setIpCertApplyTarget(v string) error {
	return s.setString(settingKeyIpCertApplyTarget, v)
}

func (s *SettingService) getIpCertAccountKey() (string, error) {
	return s.getString(settingKeyIpCertAccountKey)
}

func (s *SettingService) setIpCertAccountKey(v string) error {
	return s.setEncryptedString(settingKeyIpCertAccountKey, v)
}

func (s *SettingService) getIpCertAccountRegistration() (string, error) {
	return s.getString(settingKeyIpCertAccountRegistration)
}

func (s *SettingService) setIpCertAccountRegistration(v string) error {
	return s.setString(settingKeyIpCertAccountRegistration, v)
}

func (s *SettingService) getIpCertLastIP() (string, error) {
	return s.getString(settingKeyIpCertLastIP)
}

func (s *SettingService) setIpCertLastIP(v string) error {
	return s.setString(settingKeyIpCertLastIP, v)
}

func (s *SettingService) GetIpCertCertPath() (string, error) {
	return s.getString(settingKeyIpCertCertPath)
}

func (s *SettingService) setIpCertCertPath(v string) error {
	return s.setString(settingKeyIpCertCertPath, v)
}

func (s *SettingService) GetIpCertKeyPath() (string, error) {
	return s.getString(settingKeyIpCertKeyPath)
}

func (s *SettingService) setIpCertKeyPath(v string) error {
	return s.setString(settingKeyIpCertKeyPath, v)
}

func (s *SettingService) GetIpCertNotAfter() (string, error) {
	return s.getString(settingKeyIpCertNotAfter)
}

func (s *SettingService) setIpCertNotAfter(v string) error {
	return s.setString(settingKeyIpCertNotAfter, v)
}

func (s *SettingService) GetIpCertLastIssue() (string, error) {
	return s.getString(settingKeyIpCertLastIssue)
}

func (s *SettingService) setIpCertLastIssue(v string) error {
	return s.setString(settingKeyIpCertLastIssue, v)
}
