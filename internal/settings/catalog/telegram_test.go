package catalog

import "testing"

func TestTelegramDefaults(t *testing.T) {
	defaults := TelegramDefaults()
	if defaults[TelegramTransportModeKey] != "proxy" {
		t.Fatalf("telegram transport mode default = %q", defaults[TelegramTransportModeKey])
	}
	if defaults[TelegramCPUThresholdKey] != "90" {
		t.Fatalf("telegram CPU threshold default = %q", defaults[TelegramCPUThresholdKey])
	}
	if defaults[TelegramBackupExcludeTablesKey] != "stats,client_ips,audit_events,changes" {
		t.Fatalf("telegram backup exclude tables default = %q", defaults[TelegramBackupExcludeTablesKey])
	}
}

func TestTelegramKeyGroups(t *testing.T) {
	if _, ok := TelegramBooleanKeys()[TelegramNotifyCPUKey]; !ok {
		t.Fatal("telegram notify CPU should be in boolean group")
	}
	if _, ok := TelegramBooleanKeys()[TelegramEnabledKey]; ok {
		t.Fatal("telegram enabled was not in the legacy boolean group")
	}
	if _, ok := TelegramEncryptedKeys()[TelegramBotTokenKey]; !ok {
		t.Fatal("telegram bot token should be encrypted")
	}
	if _, ok := TelegramEncryptedKeys()[TelegramBackupPassphraseKey]; !ok {
		t.Fatal("telegram backup passphrase should be encrypted")
	}
}
