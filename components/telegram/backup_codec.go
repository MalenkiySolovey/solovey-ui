//go:build !minimal

package telegram

import (
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/backupcodec"
	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-gonic/gin"
)

func registerBackupCodecs() func() {
	unregisterExport := backupcodec.RegisterExport(id, backupcodec.ExportCodec{
		Selected:     telegramBackupExportSelected,
		Preflight:    telegramBackupExportPreflight,
		Encode:       encodeTelegramBackupEnvelope,
		EncodeStream: encodeTelegramBackupEnvelopeStream,
	})
	unregisterImport := backupcodec.RegisterImport(id, backupcodec.ImportCodec{
		HeaderBytes:       len(backupenvelope.Magic),
		PassphraseFields:  []string{"telegramBackupPassphrase"},
		Match:             backupenvelope.IsEnvelope,
		Decode:            decodeTelegramBackupEnvelope,
		DecodeStream:      decodeTelegramBackupEnvelopeStream,
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

func encodeTelegramBackupEnvelopeStream(ctx backupcodec.ExportStreamContext) (backupcodec.ExportResult, error) {
	passphrase, err := (telegramsettings.Reader{}).GetTelegramBackupPassphraseBytes()
	if err != nil {
		return backupcodec.ExportResult{}, backupcodec.NewError(http.StatusInternalServerError, "settings", err)
	}
	defer common.WipeBytes(passphrase)
	if len(passphrase) == 0 {
		return backupcodec.ExportResult{}, backupcodec.NewError(http.StatusBadRequest, "missing_passphrase", nil)
	}
	plainBytes, payloadBytes, err := backupenvelope.SealStream(ctx.Destination, ctx.Plain, passphrase)
	if err != nil {
		return backupcodec.ExportResult{}, backupcodec.NewError(http.StatusInternalServerError, "encryption_failed", err)
	}
	return backupcodec.ExportResult{Encrypted: true, PlainBytes: plainBytes, PayloadBytes: payloadBytes,
		AuditEvent: "tg_backup_manual_encrypted", AuditSeverity: service.AuditSeverityInfo,
		AuditDetails: map[string]any{"channel": "local_download", "payloadSizeBytes": plainBytes,
			"envelopeSizeBytes": payloadBytes, "excludedTables": backup.ParseExcludes(ctx.Exclude)}}, nil
}

func telegramBackupExportSelected(c *gin.Context) bool {
	return c.Query("backupEncryption") == id || c.Query("backupCodec") == id
}

func telegramBackupExportPreflight(*gin.Context) error {
	hasPassphrase, err := (telegramsettings.Reader{}).HasTelegramBackupPassphrase()
	if err != nil {
		return backupcodec.NewError(http.StatusInternalServerError, "settings", err)
	}
	if !hasPassphrase {
		return backupcodec.NewError(http.StatusBadRequest, "missing_passphrase", nil)
	}
	return nil
}

func encodeTelegramBackupEnvelope(ctx backupcodec.ExportContext) (backupcodec.ExportResult, error) {
	passphrase, err := (telegramsettings.Reader{}).GetTelegramBackupPassphraseBytes()
	if err != nil {
		return backupcodec.ExportResult{}, backupcodec.NewError(http.StatusInternalServerError, "settings", err)
	}
	defer common.WipeBytes(passphrase)
	if len(passphrase) == 0 {
		return backupcodec.ExportResult{}, backupcodec.NewError(http.StatusBadRequest, "missing_passphrase", nil)
	}

	envelope, err := backupenvelope.Build(ctx.Plain, passphrase)
	if err != nil {
		return backupcodec.ExportResult{}, backupcodec.NewError(http.StatusInternalServerError, "encryption_failed", err)
	}
	return backupcodec.ExportResult{
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

func decodeTelegramBackupEnvelope(ctx backupcodec.ImportContext) ([]byte, error) {
	passphrase := telegramBackupRestorePassphrase(ctx.Gin)
	defer common.WipeBytes(passphrase)
	if len(passphrase) == 0 {
		return nil, backupcodec.NewError(http.StatusBadRequest, "decryption_failed", nil)
	}
	plaintext, err := backupenvelope.Open(ctx.Payload, passphrase)
	if err != nil {
		return nil, backupcodec.NewError(http.StatusBadRequest, "decryption_failed", err)
	}
	return plaintext, nil
}

func decodeTelegramBackupEnvelopeStream(ctx backupcodec.ImportStreamContext) error {
	passphrase := telegramBackupRestorePassphrase(ctx.Gin)
	defer common.WipeBytes(passphrase)
	if len(passphrase) == 0 || ctx.Source == nil || ctx.Destination == nil {
		return backupcodec.NewError(http.StatusBadRequest, "decryption_failed", nil)
	}
	prefix := make([]byte, len(backupenvelope.Magic)+1)
	if _, err := io.ReadFull(ctx.Source, prefix); err != nil {
		return backupcodec.NewError(http.StatusBadRequest, "decryption_failed", err)
	}
	if _, err := ctx.Source.Seek(0, io.SeekStart); err != nil {
		return backupcodec.NewError(http.StatusBadRequest, "decryption_failed", err)
	}
	if prefix[len(backupenvelope.Magic)] == backupenvelope.VersionStream {
		if _, _, err := backupenvelope.OpenStream(ctx.Destination, ctx.Source, passphrase, ctx.MaxBytes); err != nil {
			return backupcodec.NewError(http.StatusBadRequest, "decryption_failed", err)
		}
		return nil
	}
	const legacyEnvelopeMaxBytes = 32 << 20
	payload, err := io.ReadAll(io.LimitReader(ctx.Source, legacyEnvelopeMaxBytes+1))
	if err != nil || len(payload) > legacyEnvelopeMaxBytes {
		common.WipeBytes(payload)
		return backupcodec.NewError(http.StatusBadRequest, "decryption_failed", errors.Join(err, errors.New("legacy envelope exceeds memory bound")))
	}
	defer common.WipeBytes(payload)
	plaintext, err := backupenvelope.Open(payload, passphrase)
	if err != nil || int64(len(plaintext)) > ctx.MaxBytes {
		common.WipeBytes(plaintext)
		return backupcodec.NewError(http.StatusBadRequest, "decryption_failed", err)
	}
	defer common.WipeBytes(plaintext)
	if _, err := ctx.Destination.Write(plaintext); err != nil {
		return backupcodec.NewError(http.StatusInternalServerError, "decryption_failed", err)
	}
	return nil
}

func telegramBackupRestorePassphrase(c *gin.Context) []byte {
	passphraseValue := c.PostForm("telegramBackupPassphrase")
	if passphraseValue == "" {
		passphraseValue = c.PostForm("backupPassphrase")
	}
	return []byte(passphraseValue)
}
