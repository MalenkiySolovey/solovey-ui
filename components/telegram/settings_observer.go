//go:build !minimal

package telegram

import (
	"encoding/json"

	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func registerSettingsObserver(runtime *service.Runtime) func() {
	return service.RegisterConfigSaveObserver(id, func(ctx service.ConfigSaveObserverContext) (service.ConfigSaveAfterCommit, error) {
		return telegramBackupPassphraseAuditObserver(runtime, ctx)
	})
}

func telegramBackupPassphraseAuditObserver(runtime *service.Runtime, ctx service.ConfigSaveObserverContext) (service.ConfigSaveAfterCommit, error) {
	if ctx.Object != "settings" {
		return nil, nil
	}
	var settings map[string]string
	if err := json.Unmarshal(ctx.Data, &settings); err != nil {
		return nil, err
	}
	newPassphrase, ok := settings[telegramsettings.BackupPassphraseKey]
	if !ok || newPassphrase == service.StoredSecretMarker {
		return nil, nil
	}
	oldPassphrase, err := (telegramsettings.Reader{}).GetTelegramBackupPassphraseBytes()
	if err != nil {
		return nil, err
	}
	defer common.WipeBytes(oldPassphrase)
	if string(oldPassphrase) == newPassphrase {
		return nil, nil
	}
	configured := newPassphrase != ""
	return func() {
		if err := (&service.AuditService{Runtime: runtime}).Record(service.AuditEvent{
			Actor:    ctx.LoginUser,
			Event:    "tg_backup_passphrase_changed",
			Resource: "database",
			Severity: service.AuditSeverityInfo,
			Details: map[string]any{
				"configured": configured,
			},
		}); err != nil {
			logger.Warning("telegram backup passphrase audit failed:", err)
		}
	}, nil
}
