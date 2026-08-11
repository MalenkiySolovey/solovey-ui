//go:build !minimal

package telegram

import (
	"testing"

	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func TestTelegramOwnsCompatiblePanelSettingAliases(t *testing.T) {
	cleanup := registerSettingContribution()
	t.Cleanup(cleanup)

	want := map[string]string{
		"tgBotEnable": telegramsettings.EnabledKey,
		"tgBotToken":  telegramsettings.BotTokenKey,
		"tgBotChatId": telegramsettings.ChatIDKey,
		"tgRunTime":   telegramsettings.ReportCronKey,
		"tgCpu":       telegramsettings.CPUThresholdKey,
		"tgBotBackup": telegramsettings.BackupEnabledKey,
		"tgBotProxy":  telegramsettings.ProxyURLKey,
	}
	got := service.CurrentSettingImportAliases()
	for alias, target := range want {
		if got[alias] != target {
			t.Fatalf("import alias %q = %q, want %q", alias, got[alias], target)
		}
	}

	cleanup()
	for alias := range want {
		if _, exists := service.CurrentSettingImportAliases()[alias]; exists {
			t.Fatalf("import alias %q survived component cleanup", alias)
		}
	}
}
