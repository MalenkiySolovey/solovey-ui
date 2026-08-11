//go:build minimal

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
