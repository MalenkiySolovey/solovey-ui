//go:build !minimal

package api

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/service"
)

func registerFixtureSettingsContributionForTest(t *testing.T) {
	t.Helper()
	unregister := service.RegisterSettingContribution("test.fixture."+t.Name(), service.SettingContribution{
		Defaults: map[string]string{
			"fixtureEnabled":             "false",
			"fixtureBotToken":            "",
			"fixtureChatID":              "",
			"fixtureBackupPassphrase":    "",
			"fixtureBackupExcludeTables": "stats,client_ips,audit_events,changes",
			"fixtureBackupMaxSizeMB":     "45",
		},
		Encrypted: map[string]struct{}{
			"fixtureBotToken":         {},
			"fixtureBackupPassphrase": {},
		},
		ClearableEmptyEncrypted: map[string]struct{}{
			"fixtureBackupPassphrase": {},
		},
	})
	t.Cleanup(unregister)
}

// Black-box component route tests intentionally exercise the compiled
// Telegram API without importing its package. The host fixture mirrors only
// the settings that those route contracts consume.
func registerTelegramSettingsContributionForTest(t *testing.T) {
	t.Helper()
	unregister := service.RegisterSettingContribution("test.telegram-route."+t.Name(), service.SettingContribution{
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
