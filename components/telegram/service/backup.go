//go:build !minimal

package telegram

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"

	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"
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
	Settings           BackupSettings
	Telegram           *Service
	Audit              func(AuditRecord) error
	SendDocumentStream func(context.Context, string, io.Reader, string) Result
}

type telegramBackupActorContextKey struct{}

var telegramBackupRunMu sync.Mutex

type telegramBackupSecretBag struct {
	passphrase []byte
}

func (b *telegramBackupSecretBag) setPassphrase(passphrase []byte) {
	b.zeroPassphrase()
	b.passphrase = passphrase
}

func (b *telegramBackupSecretBag) zeroPassphrase() {
	common.WipeBytes(b.passphrase)
	b.passphrase = nil
}

func (b *telegramBackupSecretBag) zero() {
	b.zeroPassphrase()
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

	backupPath, cleanupBackup, err := backup.PrepareExportContext(ctx, exclude)
	if err != nil {
		result.ErrorClass = "db_snapshot_failed"
		return result
	}
	defer cleanupBackup()
	backupFile, err := os.Open(backupPath) // #nosec G304 -- generated bounded backup path.
	if err != nil {
		result.ErrorClass = "db_snapshot_failed"
		return result
	}
	var secrets telegramBackupSecretBag
	defer secrets.zero()

	passphrase, err := s.Settings.GetTelegramBackupPassphraseBytes()
	if err != nil {
		_ = backupFile.Close()
		result.ErrorClass = "settings"
		return result
	}
	secrets.setPassphrase(passphrase)
	if len(secrets.passphrase) == 0 {
		_ = backupFile.Close()
		result.ErrorClass = "missing_passphrase"
		return result
	}
	envelopeFile, err := os.CreateTemp(filepath.Dir(backupPath), "s-ui-telegram-backup-*.aes")
	if err != nil {
		_ = backupFile.Close()
		result.ErrorClass = "encryption_failed"
		return result
	}
	envelopePath := envelopeFile.Name()
	defer func() { _ = os.Remove(envelopePath) }()
	plainBytes, envelopeBytes, sealErr := backupenvelope.SealStream(envelopeFile, &telegramContextReader{ctx: ctx, reader: backupFile}, secrets.passphrase)
	secrets.zeroPassphrase()
	syncErr, envelopeCloseErr, backupCloseErr := envelopeFile.Sync(), envelopeFile.Close(), backupFile.Close()
	if sealErr != nil || syncErr != nil || envelopeCloseErr != nil || backupCloseErr != nil {
		result.ErrorClass = "encryption_failed"
		return result
	}
	result.PayloadSizeBytes, result.EnvelopeSizeBytes = plainBytes, envelopeBytes

	maxBytes := int64(maxSizeMB) * 1024 * 1024
	if envelopeBytes > maxBytes {
		result.ErrorClass = "oversize"
		return result
	}

	now := time.Now().UTC()
	filename := telegramBackupFilename(now)
	caption := telegramBackupCaption(now, trigger, result.ExcludedTables)
	send := s.SendDocumentStream
	if send == nil && s.Telegram != nil {
		send = s.Telegram.SendDocumentStream
	}
	if send == nil {
		result.ErrorClass = "internal"
		return result
	}
	envelopeInput, err := os.Open(envelopePath) // #nosec G304 -- generated encrypted backup path.
	if err != nil {
		result.ErrorClass = "internal"
		return result
	}
	sendResult := send(ctx, filename, envelopeInput, caption)
	_ = envelopeInput.Close()
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

type telegramContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *telegramContextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
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
	return "Solovey UI-backup-" + now.Format("20060102-150405Z") + ".db.aes"
}

func telegramBackupCaption(now time.Time, trigger string, excludedTables []string) string {
	excluded := "none"
	if len(excludedTables) > 0 {
		excluded = strings.Join(excludedTables, ",")
	}
	return redact.String("Solovey UI encrypted database backup\ncreatedAt: " +
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
