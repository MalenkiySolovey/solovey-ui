package validation

import (
	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
	"github.com/MalenkiySolovey/solovey-ui/util/ssrf"
)

func ValidateProxyURLSetting(key string, value string, storedSecretMarker string) error {
	if key != settingcatalog.TelegramProxyURLKey && key != settingcatalog.PaidSubProxyURLKey {
		return nil
	}
	if value == "" || value == storedSecretMarker {
		return nil
	}
	return ssrf.ValidateProxyURL(value)
}
