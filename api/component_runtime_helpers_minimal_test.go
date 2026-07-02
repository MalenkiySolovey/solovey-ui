//go:build minimal

package api

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/service"
)

func registerTelegramSettingsContributionForTest(t *testing.T) {
	t.Helper()
	unregister := service.RegisterSettingContribution("test.telegram."+t.Name(), service.SettingContribution{
		Defaults: map[string]string{
			"telegramEnabled":             "false",
			"telegramBotToken":            "",
			"telegramChatID":              "",
			"telegramBackupPassphrase":    "",
			"telegramBackupExcludeTables": "stats,client_ips,audit_events,changes",
			"telegramBackupMaxSizeMB":     "45",
		},
		Encrypted: map[string]struct{}{
			"telegramBotToken":         {},
			"telegramBackupPassphrase": {},
		},
		ClearableEmptyEncrypted: map[string]struct{}{
			"telegramBackupPassphrase": {},
		},
	})
	t.Cleanup(unregister)
}
