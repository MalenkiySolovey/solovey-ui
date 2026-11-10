//go:build !minimal

// Package telegram sends Telegram notifications and backups using injected
// settings and runtime capabilities. It never imports the parent service package.
package telegram

import (
	"context"
	"net/http"
	"time"

	canonical "github.com/MalenkiySolovey/solovey-component-telegram/service"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

type Settings interface {
	GetTelegramEnabled() (bool, error)
	GetTelegramBotToken(context.Context) (string, error)
	GetTelegramChatID() (string, error)
	GetTelegramProxyURL() (string, error)
	GetTelegramProxyUsername() (string, error)
	GetTelegramProxyPassword() (string, error)
	GetTelegramTransportMode() (string, error)
	GetTelegramOutboundTag() (string, error)
}

type BackupSettings interface {
	Settings
	GetTelegramBackupEnabled() (bool, error)
	HasTelegramBackupPassphrase() (bool, error)
	GetTelegramBackupExcludeTables() (string, error)
	GetTelegramBackupMaxSizeMB() (int, error)
	GetTelegramBackupPassphraseBytes() ([]byte, error)
}

type AuditRecord = canonical.AuditRecord

type RuntimeProvider interface {
	CoreHTTPClient(tag string, timeout time.Duration) (*http.Client, error)
}

type Service struct {
	Settings Settings
	Runtime  RuntimeProvider
	Notifier *Notifier
	Client   *http.Client
}

type Result = canonical.Result

func (s *Service) feature() *canonical.Service {
	if s == nil {
		return nil
	}
	return &canonical.Service{Settings: s.Settings, Operations: dbsqlite.AcquireOperation, ResolveClient: s.httpClient, Notifier: s.Notifier, Client: s.Client}
}
