package validation

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/internal/settings/catalog"
)

func TestClashUDPSettingDistinguishesAbsentTrueAndFalse(t *testing.T) {
	if catalog.SubscriptionDefaults()[catalog.SubClashUDPKey] != "" {
		t.Fatal("default UDP policy changed")
	}
	for _, value := range []string{"", "true", "false"} {
		if err := ValidateSubscriptionSettingInput(catalog.SubClashUDPKey, value); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"0", "1", "TRUE", "null", " false"} {
		if err := ValidateSubscriptionSettingInput(catalog.SubClashUDPKey, value); err == nil {
			t.Fatal("ambiguous UDP setting accepted")
		}
	}
}
