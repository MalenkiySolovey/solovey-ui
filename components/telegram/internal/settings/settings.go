//go:build !minimal

// Package settings binds Telegram's definitions to Panel settings and SecretRef storage.
package settings

import (
	"context"

	canonical "github.com/MalenkiySolovey/solovey-component-telegram/settings"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

const (
	EnabledKey             = canonical.EnabledKey
	BotTokenKey            = canonical.BotTokenKey
	ChatIDKey              = canonical.ChatIDKey
	ProxyURLKey            = canonical.ProxyURLKey
	ProxyUsernameKey       = canonical.ProxyUsernameKey
	ProxyPasswordKey       = canonical.ProxyPasswordKey
	TransportModeKey       = canonical.TransportModeKey
	OutboundTagKey         = canonical.OutboundTagKey
	CPUThresholdKey        = canonical.CPUThresholdKey
	NotifyCPUKey           = canonical.NotifyCPUKey
	ReportKey              = canonical.ReportKey
	ReportCronKey          = canonical.ReportCronKey
	BackupEnabledKey       = canonical.BackupEnabledKey
	BackupPassphraseKey    = canonical.BackupPassphraseKey
	BackupCronKey          = canonical.BackupCronKey
	BackupExcludeTablesKey = canonical.BackupExcludeTablesKey
	BackupMaxSizeMBKey     = canonical.BackupMaxSizeMBKey
)

type Reader struct{ service.SettingService }

func Defaults() map[string]string        { return canonical.Defaults() }
func AllKeys() []string                  { return canonical.AllKeys() }
func BooleanKeys() map[string]struct{}   { return canonical.BooleanKeys() }
func EncryptedKeys() map[string]struct{} { return canonical.EncryptedKeys() }
func ClearableEmptyEncryptedKeys() map[string]struct{} {
	return canonical.ClearableEmptyEncryptedKeys()
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

func (r Reader) GetTelegramBotToken(ctx context.Context) (string, error) {
	intentID, err := common.RandomUUID()
	if err != nil {
		return "", secretref.ErrUnavailable
	}
	var token string
	_, err = r.AccessComponentSecret(ctx, secretref.Access{Reference: secretref.NewReference("telegram", BotTokenKey),
		Actor: secretref.Actor{Component: "telegram"}, Operation: secretref.UseForOwner, IntentID: intentID}, func(value []byte) error {
		token = string(value)
		return nil
	})
	return token, err
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
