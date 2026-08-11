//go:build !minimal

// Package telegram sends Telegram notifications and backups using injected
// settings and runtime capabilities. It never imports the parent service package.
package telegram

import (
	"net/http"
	"time"
)

type Settings interface {
	GetTelegramEnabled() (bool, error)
	GetTelegramBotToken() (string, error)
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

type AuditRecord struct {
	Actor    string
	Event    string
	Resource string
	Severity string
	Details  map[string]any
}

type RuntimeProvider interface {
	CoreHTTPClient(tag string, timeout time.Duration) (*http.Client, error)
}

type Service struct {
	Settings Settings
	Runtime  RuntimeProvider
	Notifier *Notifier
	Client   *http.Client
}

type Result struct {
	Success    bool          `json:"success"`
	ErrorClass string        `json:"errorClass,omitempty"`
	RetryAfter time.Duration `json:"-"`
}
