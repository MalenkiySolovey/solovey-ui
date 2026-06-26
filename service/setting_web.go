package service

import (
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	settingsvalidation "github.com/MalenkiySolovey/solovey-ui/internal/settings/validation"
)

func (s *SettingService) GetListen() (string, error) {
	return s.getString(settingcatalog.WebListenKey)
}

func (s *SettingService) GetWebDomain() (string, error) {
	return s.getString(settingcatalog.WebDomainKey)
}

func (s *SettingService) GetWebURI() (string, error) {
	return s.getString(settingcatalog.WebURIKey)
}

// ClearWebDomainAndAddress clears the panel domain, listen address and web URI.
// It restores access by IP on all interfaces when a wrong domain or listen
// address was configured and locked the panel out. A panel restart is required
// for the change to take effect.
func (s *SettingService) ClearWebDomainAndAddress() error {
	for _, key := range []string{settingcatalog.WebDomainKey, settingcatalog.WebListenKey, settingcatalog.WebURIKey} {
		if err := s.setString(key, ""); err != nil {
			return err
		}
	}
	return nil
}

func (s *SettingService) GetPort() (int, error) {
	return s.getInt(settingcatalog.WebPortKey)
}

func (s *SettingService) SetPort(port int) error {
	return s.setInt(settingcatalog.WebPortKey, port)
}

func (s *SettingService) GetCertFile() (string, error) {
	return s.getString(settingcatalog.WebCertFileKey)
}

func (s *SettingService) GetKeyFile() (string, error) {
	return s.getString(settingcatalog.WebKeyFileKey)
}

func (s *SettingService) GetWebPath() (string, error) {
	webPath, err := s.getString(settingcatalog.WebPathKey)
	if err != nil {
		return "", err
	}
	return settingsvalidation.NormalizeURLPath(webPath), nil
}

func (s *SettingService) SetWebPath(webPath string) error {
	webPath, err := settingsvalidation.NormalizeAndValidatePathSetting(settingcatalog.WebPathKey, webPath)
	if err != nil {
		return err
	}
	return s.setString(settingcatalog.WebPathKey, webPath)
}
