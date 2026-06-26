package service

import settingsvalidation "github.com/MalenkiySolovey/solovey-ui/internal/settings/validation"

func (s *SettingService) GetSubListen() (string, error) {
	return s.getString(settingKeySubListen)
}

func (s *SettingService) GetSubPort() (int, error) {
	return s.getInt(settingKeySubPort)
}

func (s *SettingService) SetSubPort(subPort int) error {
	return s.setInt(settingKeySubPort, subPort)
}

func (s *SettingService) GetSubPath() (string, error) {
	return s.getNormalizedSubPath(settingKeySubPath)
}

func (s *SettingService) getNormalizedSubPath(key string) (string, error) {
	subPath, err := s.getString(key)
	if err != nil {
		return "", err
	}
	return settingsvalidation.NormalizeURLPath(subPath), nil
}

func (s *SettingService) SetSubPath(subPath string) error {
	subPath, err := settingsvalidation.NormalizeAndValidatePathSetting(settingKeySubPath, subPath)
	if err != nil {
		return err
	}
	return s.setString(settingKeySubPath, subPath)
}

func (s *SettingService) GetSubDomain() (string, error) {
	return s.getString(settingKeySubDomain)
}

func (s *SettingService) GetSubCertFile() (string, error) {
	return s.getString(settingKeySubCertFile)
}

func (s *SettingService) GetSubKeyFile() (string, error) {
	return s.getString(settingKeySubKeyFile)
}

func (s *SettingService) GetSubUpdates() (int, error) {
	return s.getInt(settingKeySubUpdates)
}

func (s *SettingService) GetSubEncode() (bool, error) {
	return s.getBool(settingKeySubEncode)
}

func (s *SettingService) GetSubShowInfo() (bool, error) {
	return s.getBool(settingKeySubShowInfo)
}

func (s *SettingService) GetSubSecretRequired() (bool, error) {
	return s.getBool(settingKeySubSecretRequired)
}

func (s *SettingService) GetSubRateLimitPerIP() (int, error) {
	return s.getInt(settingKeySubRateLimitPerIP)
}

func (s *SettingService) GetSubLinkEnable() (bool, error) {
	return s.getBool(settingKeySubLinkEnable)
}

func (s *SettingService) GetSubJsonEnable() (bool, error) {
	return s.getBool(settingKeySubJsonEnable)
}

func (s *SettingService) GetSubClashEnable() (bool, error) {
	return s.getBool(settingKeySubClashEnable)
}

func (s *SettingService) GetSubXrayEnable() (bool, error) {
	return s.getBool(settingKeySubXrayEnable)
}

func (s *SettingService) GetSubRemoteGroupAdaptation() (string, error) {
	return s.getString(settingKeySubRemoteGroupAdaptation)
}

func (s *SettingService) GetSubRemoteConversionPolicy() (string, error) {
	return s.getString(settingKeySubRemoteConversionPolicy)
}

func (s *SettingService) GetSubJsonPath() (string, error) {
	return s.getNormalizedSubPath(settingKeySubJsonPath)
}

func (s *SettingService) GetSubClashPath() (string, error) {
	return s.getNormalizedSubPath(settingKeySubClashPath)
}

func (s *SettingService) GetSubXrayPath() (string, error) {
	return s.getNormalizedSubPath(settingKeySubXrayPath)
}

func (s *SettingService) GetSubJsonURI() (string, error) {
	return s.getString(settingKeySubJsonURI)
}

func (s *SettingService) GetSubClashURI() (string, error) {
	return s.getString(settingKeySubClashURI)
}

func (s *SettingService) GetSubXrayURI() (string, error) {
	return s.getString(settingKeySubXrayURI)
}

func (s *SettingService) GetSubTitle() (string, error) {
	return s.getString(settingKeySubTitle)
}

func (s *SettingService) GetSubSupportUrl() (string, error) {
	return s.getString(settingKeySubSupportURL)
}

func (s *SettingService) GetSubProfileUrl() (string, error) {
	return s.getString(settingKeySubProfileURL)
}

func (s *SettingService) GetSubAnnounce() (string, error) {
	return s.getString(settingKeySubAnnounce)
}

func (s *SettingService) GetSubNameInRemark() (bool, error) {
	return s.getBool(settingKeySubNameInRemark)
}

func (s *SettingService) GetSubJsonFragment() (string, error) {
	return s.getString(settingKeySubJsonFragment)
}

func (s *SettingService) GetSubJsonNoises() (string, error) {
	return s.getString(settingKeySubJsonNoises)
}

func (s *SettingService) GetSubJsonMux() (bool, error) {
	return s.getBool(settingKeySubJsonMux)
}

func (s *SettingService) GetSubJsonDirectRules() (bool, error) {
	return s.getBool(settingKeySubJsonDirectRules)
}

func (s *SettingService) GetSubURI() (string, error) {
	return s.getString(settingKeySubURI)
}

func (s *SettingService) GetSubJsonExt() (string, error) {
	return s.getString(settingKeySubJsonExt)
}

func (s *SettingService) GetSubClashExt() (string, error) {
	return s.getString(settingKeySubClashExt)
}
