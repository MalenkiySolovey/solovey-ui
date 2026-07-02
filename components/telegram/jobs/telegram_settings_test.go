//go:build !minimal

package jobs

import (
	"testing"

	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func registerTelegramSettingsForTest(t *testing.T) {
	t.Helper()
	unregister := service.RegisterSettingContribution("test.telegram", service.SettingContribution{
		Defaults:                telegramsettings.Defaults(),
		Encrypted:               telegramsettings.EncryptedKeys(),
		ClearableEmptyEncrypted: telegramsettings.ClearableEmptyEncryptedKeys(),
	})
	t.Cleanup(unregister)
}
