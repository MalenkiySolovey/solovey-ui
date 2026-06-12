package service

import (
	"os"

	"github.com/MalenkiySolovey/solovey-ui/util"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func (s *SettingService) validateEndpointSettingInput(key string, value string) error {
	if isDomainSetting(key) {
		if err := util.ValidateHostname(value); err != nil {
			return common.NewErrorf("%s: %v", key, err)
		}
	}
	if value != "" && isCertificatePathSetting(key) {
		if err := s.fileExists(value); err != nil {
			return common.NewError(" -> ", value, " is not exists")
		}
	}
	if isPathSetting(key) {
		if _, err := normalizeAndValidatePathSetting(key, value); err != nil {
			return err
		}
	}
	return nil
}

func isCertificatePathSetting(key string) bool {
	switch key {
	case "webCertFile", "webKeyFile", settingKeySubCertFile, settingKeySubKeyFile:
		return true
	default:
		return false
	}
}

func isDomainSetting(key string) bool {
	switch key {
	case "webDomain", settingKeySubDomain:
		return true
	default:
		return false
	}
}

func (s *SettingService) fileExists(path string) error {
	_, err := os.Stat(path)
	return err
}
