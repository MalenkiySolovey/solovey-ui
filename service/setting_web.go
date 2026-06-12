package service

import "strings"

func (s *SettingService) GetListen() (string, error) {
	return s.getString("webListen")
}

func (s *SettingService) GetWebDomain() (string, error) {
	return s.getString("webDomain")
}

func (s *SettingService) GetWebURI() (string, error) {
	return s.getString("webURI")
}

// ClearWebDomainAndAddress clears the panel domain, listen address and web URI.
// It restores access by IP on all interfaces when a wrong domain or listen
// address was configured and locked the panel out. A panel restart is required
// for the change to take effect.
func (s *SettingService) ClearWebDomainAndAddress() error {
	for _, key := range []string{"webDomain", "webListen", "webURI"} {
		if err := s.setString(key, ""); err != nil {
			return err
		}
	}
	return nil
}

func (s *SettingService) GetPort() (int, error) {
	return s.getInt("webPort")
}

func (s *SettingService) SetPort(port int) error {
	return s.setInt("webPort", port)
}

func (s *SettingService) GetCertFile() (string, error) {
	return s.getString("webCertFile")
}

func (s *SettingService) GetKeyFile() (string, error) {
	return s.getString("webKeyFile")
}

func (s *SettingService) GetWebPath() (string, error) {
	webPath, err := s.getString("webPath")
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(webPath, "/") {
		webPath = "/" + webPath
	}
	if !strings.HasSuffix(webPath, "/") {
		webPath += "/"
	}
	return webPath, nil
}

func (s *SettingService) SetWebPath(webPath string) error {
	webPath, err := normalizeAndValidatePathSetting("webPath", webPath)
	if err != nil {
		return err
	}
	return s.setString("webPath", webPath)
}
