package validation

import (
	"testing"

	settingcatalog "github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
)

func TestValidateSessionSettingInput(t *testing.T) {
	if err := ValidateSessionSettingInput(settingcatalog.ForceCookieSecureKey, "true"); err != nil {
		t.Fatalf("valid forceCookieSecure returned error: %v", err)
	}
	if err := ValidateSessionSettingInput(settingcatalog.SessionSameSiteStrictKey, "maybe"); err == nil {
		t.Fatal("invalid session boolean succeeded, want error")
	}
	if err := ValidateSessionSettingInput("unknown", "anything"); err != nil {
		t.Fatalf("unknown session key returned error: %v", err)
	}
}

func TestValidateRuntimeSettingInput(t *testing.T) {
	if err := ValidateRuntimeSettingInput(settingcatalog.ObservabilityMemoryCapMBKey, "64"); err != nil {
		t.Fatalf("valid observability cap returned error: %v", err)
	}
	for _, value := range []string{"0", "-1", "1025", "not-int"} {
		if err := ValidateRuntimeSettingInput(settingcatalog.ObservabilityMemoryCapMBKey, value); err == nil {
			t.Fatalf("ValidateRuntimeSettingInput(%q) = nil, want error", value)
		}
	}
}
