package api

import (
	"net/http"
	"testing"

	dbtransferhttp "github.com/MalenkiySolovey/solovey-ui/api/dbtransfer"
	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-gonic/gin"
)

func registerTelegramBackupTransferCodecsForTest(t *testing.T, settingService *service.SettingService) {
	t.Helper()
	dbtransferhttp.ResetBackupCodecsForTest()
	unregisterExport := dbtransferhttp.RegisterBackupExportCodec("test-telegram", dbtransferhttp.BackupExportCodec{
		Selected: func(c *gin.Context) bool {
			return c.Query("backupEncryption") == "test-telegram"
		},
		Preflight: func(*gin.Context) error {
			hasPassphrase, err := settingService.HasComponentSettingSecret("telegramBackupPassphrase")
			if err != nil {
				return dbtransferhttp.NewBackupCodecError(http.StatusInternalServerError, "settings", err)
			}
			if !hasPassphrase {
				return dbtransferhttp.NewBackupCodecError(http.StatusBadRequest, "missing_passphrase", nil)
			}
			return nil
		},
		Encode: func(ctx dbtransferhttp.BackupExportContext) (dbtransferhttp.BackupExportResult, error) {
			passphrase, err := settingService.GetComponentSettingSecretBytes("telegramBackupPassphrase")
			if err != nil {
				return dbtransferhttp.BackupExportResult{}, dbtransferhttp.NewBackupCodecError(http.StatusInternalServerError, "settings", err)
			}
			defer common.WipeBytes(passphrase)
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
		},
	})
	unregisterImport := dbtransferhttp.RegisterBackupImportCodec("test-telegram", dbtransferhttp.BackupImportCodec{
		HeaderBytes:       len(backupenvelope.Magic),
		Match:             backupenvelope.IsEnvelope,
		FailureAuditEvent: "tg_backup_restore_failed",
		Decode: func(ctx dbtransferhttp.BackupImportContext) ([]byte, error) {
			passphrase := ctx.Gin.PostForm("telegramBackupPassphrase")
			if passphrase == "" {
				passphrase = ctx.Gin.PostForm("backupPassphrase")
			}
			if passphrase == "" {
				return nil, dbtransferhttp.NewBackupCodecError(http.StatusBadRequest, "decryption_failed", nil)
			}
			return backupenvelope.Open(ctx.Payload, []byte(passphrase))
		},
	})
	t.Cleanup(func() {
		unregisterImport()
		unregisterExport()
		dbtransferhttp.ResetBackupCodecsForTest()
	})
}
