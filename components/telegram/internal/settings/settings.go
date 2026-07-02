//go:build !minimal

// Package settings owns Telegram component setting keys, defaults, and readers.
package settings

import "github.com/MalenkiySolovey/solovey-ui/service"

const (
	EnabledKey             = "telegramEnabled"
	BotTokenKey            = "telegramBotToken"
	ChatIDKey              = "telegramChatID"
	ProxyURLKey            = "telegramProxyURL"
	ProxyUsernameKey       = "telegramProxyUsername"
	ProxyPasswordKey       = "telegramProxyPassword"
	TransportModeKey       = "telegramTransportMode"
	OutboundTagKey         = "telegramOutboundTag"
	CPUThresholdKey        = "telegramCpuThreshold"
	NotifyCPUKey           = "telegramNotifyCpu"
	ReportKey              = "telegramReport"
	ReportCronKey          = "telegramReportCron"
	BackupEnabledKey       = "telegramBackupEnabled"
	BackupPassphraseKey    = "telegramBackupPassphrase"
	BackupCronKey          = "telegramBackupCron"
	BackupExcludeTablesKey = "telegramBackupExcludeTables"
	BackupMaxSizeMBKey     = "telegramBackupMaxSizeMB"
)

type Reader struct {
	service.SettingService
}

func Defaults() map[string]string {
	return map[string]string{
		EnabledKey:             "false",
		BotTokenKey:            "",
		ChatIDKey:              "",
		ProxyURLKey:            "",
		ProxyUsernameKey:       "",
		ProxyPasswordKey:       "",
		TransportModeKey:       "proxy",
		OutboundTagKey:         "",
		CPUThresholdKey:        "90",
		NotifyCPUKey:           "false",
		ReportKey:              "false",
		ReportCronKey:          "",
		BackupEnabledKey:       "false",
		BackupPassphraseKey:    "",
		BackupCronKey:          "",
		BackupExcludeTablesKey: "stats,client_ips,audit_events,changes",
		BackupMaxSizeMBKey:     "45",
	}
}

func BooleanKeys() map[string]struct{} {
	return keySet(
		NotifyCPUKey,
		ReportKey,
	)
}

func EncryptedKeys() map[string]struct{} {
	return keySet(
		BackupPassphraseKey,
		BotTokenKey,
		ProxyPasswordKey,
		ProxyURLKey,
		ProxyUsernameKey,
	)
}

func ClearableEmptyEncryptedKeys() map[string]struct{} {
	return keySet(BackupPassphraseKey)
}

func (r Reader) GetTelegramCpuThreshold() (int, error) {
	return r.GetComponentSettingInt(CPUThresholdKey)
}

func (r Reader) GetTelegramNotifyCpu() (bool, error) {
	return r.GetComponentSettingBool(NotifyCPUKey)
}

func (r Reader) GetTelegramEnabled() (bool, error) {
	return r.GetComponentSettingBool(EnabledKey)
}

func (r Reader) GetTelegramBotToken() (string, error) {
	return r.GetComponentSettingString(BotTokenKey)
}

func (r Reader) GetTelegramChatID() (string, error) {
	return r.GetComponentSettingString(ChatIDKey)
}

func (r Reader) GetTelegramProxyURL() (string, error) {
	return r.GetComponentSettingString(ProxyURLKey)
}

func (r Reader) GetTelegramProxyUsername() (string, error) {
	return r.GetComponentSettingString(ProxyUsernameKey)
}

func (r Reader) GetTelegramProxyPassword() (string, error) {
	return r.GetComponentSettingString(ProxyPasswordKey)
}

func (r Reader) GetTelegramTransportMode() (string, error) {
	return r.GetComponentSettingString(TransportModeKey)
}

func (r Reader) GetTelegramOutboundTag() (string, error) {
	return r.GetComponentSettingString(OutboundTagKey)
}

func (r Reader) GetTelegramReport() (bool, error) {
	return r.GetComponentSettingBool(ReportKey)
}

func (r Reader) GetTelegramReportCron() (string, error) {
	return r.GetComponentSettingString(ReportCronKey)
}

func (r Reader) GetTelegramBackupEnabled() (bool, error) {
	return r.GetComponentSettingBool(BackupEnabledKey)
}

func (r Reader) GetTelegramBackupCron() (string, error) {
	return r.GetComponentSettingString(BackupCronKey)
}

func (r Reader) GetTelegramBackupExcludeTables() (string, error) {
	return r.GetComponentSettingString(BackupExcludeTablesKey)
}

func (r Reader) GetTelegramBackupMaxSizeMB() (int, error) {
	return r.GetComponentSettingInt(BackupMaxSizeMBKey)
}

func (r Reader) GetTelegramBackupPassphraseBytes() ([]byte, error) {
	return r.GetComponentSettingSecretBytes(BackupPassphraseKey)
}

func (r Reader) HasTelegramBackupPassphrase() (bool, error) {
	return r.HasComponentSettingSecret(BackupPassphraseKey)
}

func keySet(keys ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		set[key] = struct{}{}
	}
	return set
}
