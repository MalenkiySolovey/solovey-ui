//go:build !minimal

package telegram

import (
	"net/http"
	"sync"

	dbtransferhttp "github.com/MalenkiySolovey/solovey-ui/api/dbtransfer"
	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-gonic/gin"
)

func registerBackupCodecs() func() {
	unregisterExport := dbtransferhttp.RegisterBackupExportCodec(id, dbtransferhttp.BackupExportCodec{
		Selected:  telegramBackupExportSelected,
		Preflight: telegramBackupExportPreflight,
		Encode:    encodeTelegramBackupEnvelope,
	})
	unregisterImport := dbtransferhttp.RegisterBackupImportCodec(id, dbtransferhttp.BackupImportCodec{
		HeaderBytes:       len(backupenvelope.Magic),
		Match:             backupenvelope.IsEnvelope,
		Decode:            decodeTelegramBackupEnvelope,
		FailureAuditEvent: "tg_backup_restore_failed",
	})

	var once sync.Once
	return func() {
		once.Do(func() {
			unregisterImport()
			unregisterExport()
		})
	}
}

func telegramBackupExportSelected(c *gin.Context) bool {
	return c.Query("backupEncryption") == id || c.Query("backupCodec") == id
}

func telegramBackupExportPreflight(*gin.Context) error {
	hasPassphrase, err := (telegramsettings.Reader{}).HasTelegramBackupPassphrase()
	if err != nil {
		return dbtransferhttp.NewBackupCodecError(http.StatusInternalServerError, "settings", err)
	}
	if !hasPassphrase {
		return dbtransferhttp.NewBackupCodecError(http.StatusBadRequest, "missing_passphrase", nil)
	}
	return nil
}

func encodeTelegramBackupEnvelope(ctx dbtransferhttp.BackupExportContext) (dbtransferhttp.BackupExportResult, error) {
	passphrase, err := (telegramsettings.Reader{}).GetTelegramBackupPassphraseBytes()
	if err != nil {
		return dbtransferhttp.BackupExportResult{}, dbtransferhttp.NewBackupCodecError(http.StatusInternalServerError, "settings", err)
	}
	defer common.WipeBytes(passphrase)
	if len(passphrase) == 0 {
		return dbtransferhttp.BackupExportResult{}, dbtransferhttp.NewBackupCodecError(http.StatusBadRequest, "missing_passphrase", nil)
	}

	envelope, err := backupenvelope.Build(ctx.Plain, passphrase)
	if err != nil {
		return dbtransferhttp.BackupExportResult{}, dbtransferhttp.NewBackupCodecError(http.StatusInternalServerError, "encryption_failed", err)
	}
	return dbtransferhttp.BackupExportResult{
		Payload:       envelope,
		Encrypted:     true,
		AuditEvent:    "tg_backup_manual_encrypted",
		AuditSeverity: service.AuditSeverityInfo,
		AuditDetails: map[string]any{
			"channel":           "local_download",
			"payloadSizeBytes":  int64(len(ctx.Plain)),
			"envelopeSizeBytes": int64(len(envelope)),
			"excludedTables":    backup.ParseExcludes(ctx.Exclude),
		},
	}, nil
}

func decodeTelegramBackupEnvelope(ctx dbtransferhttp.BackupImportContext) ([]byte, error) {
	passphrase := telegramBackupRestorePassphrase(ctx.Gin)
	defer common.WipeBytes(passphrase)
	if len(passphrase) == 0 {
		return nil, dbtransferhttp.NewBackupCodecError(http.StatusBadRequest, "decryption_failed", nil)
	}
	plaintext, err := backupenvelope.Open(ctx.Payload, passphrase)
	if err != nil {
		return nil, dbtransferhttp.NewBackupCodecError(http.StatusBadRequest, "decryption_failed", err)
	}
	return plaintext, nil
}

func telegramBackupRestorePassphrase(c *gin.Context) []byte {
	passphraseValue := c.PostForm("telegramBackupPassphrase")
	if passphraseValue == "" {
		passphraseValue = c.PostForm("backupPassphrase")
	}
	return []byte(passphraseValue)
}
