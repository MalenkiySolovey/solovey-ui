package validation

import (
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func ValidateEndpointSettingInput(key string, value string, fileExists func(path string) error) error {
	if isDomainSetting(key) {
		if err := ValidateHostname(value); err != nil {
			return common.NewErrorf("%s: %v", key, err)
		}
	}
	if value != "" && isCertificatePathSetting(key) && fileExists != nil {
		if err := fileExists(value); err != nil {
			return common.NewError(" -> ", value, " is not exists")
		}
	}
	if IsPathSetting(key) {
		if _, err := NormalizeAndValidatePathSetting(key, value); err != nil {
			return err
		}
	}
	return nil
}

func isCertificatePathSetting(key string) bool {
	switch key {
	case settingcatalog.WebCertFileKey, settingcatalog.WebKeyFileKey, settingcatalog.SubCertFileKey, settingcatalog.SubKeyFileKey:
		return true
	default:
		return false
	}
}

func isDomainSetting(key string) bool {
	switch key {
	case settingcatalog.WebDomainKey, settingcatalog.SubDomainKey:
		return true
	default:
		return false
	}
}
