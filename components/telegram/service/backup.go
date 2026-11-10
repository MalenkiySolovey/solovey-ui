//go:build !minimal

package telegram

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"

	canonical "github.com/MalenkiySolovey/solovey-component-telegram/service"
	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

const (
	TelegramBackupTriggerManual    = canonical.TelegramBackupTriggerManual
	TelegramBackupTriggerScheduled = canonical.TelegramBackupTriggerScheduled
)

type TelegramBackupResult = canonical.TelegramBackupResult
type TelegramBackupService struct {
	Settings           BackupSettings
	Telegram           *Service
	Audit              func(AuditRecord) error
	SendDocumentStream func(context.Context, string, io.Reader, string) Result
}

func ContextWithTelegramBackupActor(ctx context.Context, actor string) context.Context {
	return canonical.ContextWithTelegramBackupActor(ctx, actor)
}

func (s *TelegramBackupService) RunOnce(ctx context.Context, trigger string) TelegramBackupResult {
	if s == nil {
		return (*canonical.TelegramBackupService)(nil).RunOnce(ctx, trigger)
	}
	send := s.SendDocumentStream
	if send == nil && s.Telegram != nil {
		send = s.Telegram.SendDocumentStream
	}
	return (&canonical.TelegramBackupService{Settings: s.Settings, Operations: dbsqlite.AcquireOperation, Prepare: s.prepareEncryptedBackup, ParseExcludes: backup.ParseExcludes, Audit: s.Audit, SendDocumentStream: send, Warning: logger.Warning}).RunOnce(ctx, trigger)
}

// The existing Backup owner retains snapshot, encryption, secret zeroization
// and generated-path authority. Feature code receives only an encrypted stream.
func (s *TelegramBackupService) prepareEncryptedBackup(ctx context.Context, exclude string) (canonical.PreparedBackup, string) {
	backupPath, cleanupBackup, err := backup.PrepareExportContext(ctx, exclude)
	if err != nil {
		return canonical.PreparedBackup{}, "db_snapshot_failed"
	}
	defer cleanupBackup()
	backupFile, err := os.Open(backupPath) // #nosec G304 -- generated bounded backup path.
	if err != nil {
		return canonical.PreparedBackup{}, "db_snapshot_failed"
	}
	var secrets telegramBackupSecretBag
	defer secrets.zero()

	passphrase, err := s.Settings.GetTelegramBackupPassphraseBytes()
	if err != nil {
		_ = backupFile.Close()
		return canonical.PreparedBackup{}, "settings"
	}
	secrets.setPassphrase(passphrase)
	if len(secrets.passphrase) == 0 {
		_ = backupFile.Close()
		return canonical.PreparedBackup{}, "missing_passphrase"
	}
	envelopeFile, err := os.CreateTemp(filepath.Dir(backupPath), "s-ui-telegram-backup-*.aes")
	if err != nil {
		_ = backupFile.Close()
		return canonical.PreparedBackup{}, "encryption_failed"
	}
	envelopePath := envelopeFile.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(envelopePath)
		}
	}()
	plainBytes, envelopeBytes, sealErr := backupenvelope.SealStream(envelopeFile, &telegramContextReader{ctx: ctx, reader: backupFile}, secrets.passphrase)
	secrets.zeroPassphrase()
	syncErr, envelopeCloseErr, backupCloseErr := envelopeFile.Sync(), envelopeFile.Close(), backupFile.Close()
	if sealErr != nil || syncErr != nil || envelopeCloseErr != nil || backupCloseErr != nil {
		return canonical.PreparedBackup{}, "encryption_failed"
	}

	input, err := os.Open(envelopePath) // #nosec G304 -- generated encrypted backup path.
	if err != nil {
		return canonical.PreparedBackup{}, "internal"
	}
	keep = true
	return canonical.PreparedBackup{Content: &ownedEncryptedBackup{ReadCloser: input, cleanup: func() { _ = os.Remove(envelopePath) }}, PayloadSizeBytes: plainBytes, EnvelopeSizeBytes: envelopeBytes}, ""
}

type ownedEncryptedBackup struct {
	io.ReadCloser
	cleanup  func()
	once     sync.Once
	closeErr error
}

func (b *ownedEncryptedBackup) Close() error {
	b.once.Do(func() { b.closeErr = b.ReadCloser.Close(); b.cleanup() })
	return b.closeErr
}

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
