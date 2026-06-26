package telegram

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/internal/integrations/telegram"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/redact"
)

const (
	TelegramBackupTriggerManual    = "manual"
	TelegramBackupTriggerScheduled = "scheduled"
)

type TelegramBackupResult struct {
	Success           bool     `json:"success"`
	Filename          string   `json:"filename,omitempty"`
	Trigger           string   `json:"trigger,omitempty"`
	ErrorClass        string   `json:"errorClass,omitempty"`
	PayloadSizeBytes  int64    `json:"-"`
	EnvelopeSizeBytes int64    `json:"-"`
	ExcludedTables    []string `json:"-"`
}

type TelegramBackupService struct {
	Settings     BackupSettings
	Telegram     *Service
	Audit        func(AuditRecord) error
	SendDocument func(filename string, payload []byte, caption string) Result
}

type telegramBackupActorContextKey struct{}

var telegramBackupRunMu sync.Mutex

type telegramBackupSecretBag struct {
	payload    []byte
	passphrase []byte
}

func (b *telegramBackupSecretBag) setPayload(payload []byte) {
	b.zeroPayload()
	b.payload = payload
}

func (b *telegramBackupSecretBag) setPassphrase(passphrase []byte) {
	b.zeroPassphrase()
	b.passphrase = passphrase
}

func (b *telegramBackupSecretBag) zeroPayload() {
	common.WipeBytes(b.payload)
	b.payload = nil
}

func (b *telegramBackupSecretBag) zeroPassphrase() {
	common.WipeBytes(b.passphrase)
	b.passphrase = nil
}

func (b *telegramBackupSecretBag) zero() {
	b.zeroPassphrase()
	b.zeroPayload()
}

func ContextWithTelegramBackupActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, telegramBackupActorContextKey{}, actor)
}

func (s *TelegramBackupService) RunOnce(ctx context.Context, trigger string) (result TelegramBackupResult) {
	trigger = normalizeTelegramBackupTrigger(trigger)
	result.Trigger = trigger
	actor := telegramBackupActor(ctx, trigger)
	if !telegramBackupRunMu.TryLock() {
		result.ErrorClass = "concurrent_run"
		s.recordTelegramBackupRunAudit(actor, result)
		return result
	}
	defer telegramBackupRunMu.Unlock()
	defer func() {
		if !result.Success && result.ErrorClass == "" {
			result.ErrorClass = "internal"
		}
		s.recordTelegramBackupRunAudit(actor, result)
	}()

	if err := ctx.Err(); err != nil {
		result.ErrorClass = "internal"
		return result
	}
	telegramEnabled, err := s.Settings.GetTelegramEnabled()
	if err != nil {
		result.ErrorClass = "settings"
		return result
	}
	if !telegramEnabled {
		result.ErrorClass = "disabled"
		return result
	}
	backupEnabled, err := s.Settings.GetTelegramBackupEnabled()
	if err != nil {
		result.ErrorClass = "settings"
		return result
	}
	if !backupEnabled {
		result.ErrorClass = "disabled"
		return result
	}
	token, err := s.Settings.GetTelegramBotToken()
	if err != nil {
		result.ErrorClass = "settings"
		return result
	}
	if token == "" {
		result.ErrorClass = "missing_token"
		return result
	}
	hasPassphrase, err := s.Settings.HasTelegramBackupPassphrase()
	if err != nil {
		result.ErrorClass = "settings"
		return result
	}
	if !hasPassphrase {
		result.ErrorClass = "missing_passphrase"
		return result
	}
	exclude, err := s.Settings.GetTelegramBackupExcludeTables()
	if err != nil {
		result.ErrorClass = "settings"
		return result
	}
	result.ExcludedTables = backup.ParseExcludes(exclude)
	maxSizeMB, err := s.Settings.GetTelegramBackupMaxSizeMB()
	if err != nil {
		result.ErrorClass = "settings"
		return result
	}

	payload, err := backup.Export(exclude)
	if err != nil {
		result.ErrorClass = "db_snapshot_failed"
		return result
	}
	var secrets telegramBackupSecretBag
	secrets.setPayload(payload)
	defer secrets.zero()
	result.PayloadSizeBytes = int64(len(secrets.payload))

	passphrase, err := s.Settings.GetTelegramBackupPassphraseBytes()
	if err != nil {
		result.ErrorClass = "settings"
		return result
	}
	secrets.setPassphrase(passphrase)
	if len(secrets.passphrase) == 0 {
		result.ErrorClass = "missing_passphrase"
		return result
	}
	envelope, err := integrationtelegram.BuildTelegramBackupEnvelope(secrets.payload, secrets.passphrase)
	secrets.zeroPassphrase()
	if err != nil {
		result.ErrorClass = "encryption_failed"
		return result
	}
	secrets.zeroPayload()
	result.EnvelopeSizeBytes = int64(len(envelope))

	maxBytes := int64(maxSizeMB) * 1024 * 1024
	if int64(len(envelope)) > maxBytes {
		result.ErrorClass = "oversize"
		return result
	}

	now := time.Now().UTC()
	filename := telegramBackupFilename(now)
	caption := telegramBackupCaption(now, trigger, result.ExcludedTables)
	send := s.SendDocument
	if send == nil && s.Telegram != nil {
		send = s.Telegram.SendDocument
	}
	if send == nil {
		result.ErrorClass = "internal"
		return result
	}
	sendResult := send(filename, envelope, caption)
	if !sendResult.Success {
		result.ErrorClass = sendResult.ErrorClass
		if result.ErrorClass == "" {
			result.ErrorClass = "internal"
		}
		return result
	}
	result.Success = true
	result.Filename = filename
	return result
}

func normalizeTelegramBackupTrigger(trigger string) string {
	switch trigger {
	case TelegramBackupTriggerScheduled:
		return TelegramBackupTriggerScheduled
	default:
		return TelegramBackupTriggerManual
	}
}

func telegramBackupActor(ctx context.Context, trigger string) string {
	if actor, ok := ctx.Value(telegramBackupActorContextKey{}).(string); ok && actor != "" {
		return actor
	}
	if trigger == TelegramBackupTriggerScheduled {
		return "system"
	}
	return "unknown"
}

func telegramBackupFilename(now time.Time) string {
	return "s-ui-backup-" + now.Format("20060102-150405Z") + ".db.aes"
}

func telegramBackupCaption(now time.Time, trigger string, excludedTables []string) string {
	excluded := "none"
	if len(excludedTables) > 0 {
		excluded = strings.Join(excludedTables, ",")
	}
	return redact.String("S-UI encrypted database backup\ncreatedAt: " +
		now.Format(time.RFC3339) +
		"\nsource: " + trigger +
		"\nexcludedTables: " + excluded)
}

func (s *TelegramBackupService) recordTelegramBackupRunAudit(actor string, result TelegramBackupResult) {
	details := map[string]any{
		"trigger":           result.Trigger,
		"payloadSizeBytes":  result.PayloadSizeBytes,
		"envelopeSizeBytes": result.EnvelopeSizeBytes,
		"excludedTables":    result.ExcludedTables,
		"channel":           "telegram",
	}
	event := "tg_backup_sent"
	severity := "info"
	if !result.Success {
		event = "tg_backup_failed"
		severity = "warn"
		details["errorClass"] = result.ErrorClass
	}
	if s.Audit == nil {
		return
	}
	if err := s.Audit(AuditRecord{
		Actor:    actor,
		Event:    event,
		Resource: "database",
		Severity: severity,
		Details:  details,
	}); err != nil {
		logger.Warning("telegram backup audit failed:", err)
	}
}
