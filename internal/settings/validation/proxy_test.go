package validation

import (
	"testing"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
)

func TestValidateProxyURLSetting(t *testing.T) {
	if err := ValidateProxyURLSetting(settingcatalog.TelegramProxyURLKey, "", "stored"); err != nil {
		t.Fatalf("empty proxy URL returned error: %v", err)
	}
	if err := ValidateProxyURLSetting(settingcatalog.TelegramProxyURLKey, "stored", "stored"); err != nil {
		t.Fatalf("stored marker returned error: %v", err)
	}
	if err := ValidateProxyURLSetting(settingcatalog.PaidSubProxyURLKey, "http://8.8.8.8:8080", "stored"); err != nil {
		t.Fatalf("valid paid sub proxy URL returned error: %v", err)
	}
	if err := ValidateProxyURLSetting("unknown", "http://127.0.0.1:8080", "stored"); err != nil {
		t.Fatalf("unknown key returned error: %v", err)
	}
}
