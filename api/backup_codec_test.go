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

func registerFixtureBackupTransferCodecsForTest(t *testing.T, settingService *service.SettingService) {
	t.Helper()
	unregisterExport, err := dbtransferhttp.RegisterBackupExportCodec("test-fixture-codec", dbtransferhttp.BackupExportCodec{
		Selected: func(c *gin.Context) bool {
			return c.Query("backupEncryption") == "test-fixture-codec"
		},
		Preflight: func(*gin.Context) error {
			hasPassphrase, err := settingService.HasComponentSettingSecret("fixtureBackupPassphrase")
			if err != nil {
				return dbtransferhttp.NewBackupCodecError(http.StatusInternalServerError, "settings", err)
			}
			if !hasPassphrase {
				return dbtransferhttp.NewBackupCodecError(http.StatusBadRequest, "missing_passphrase", nil)
			}
			return nil
		},
		Encode: func(ctx dbtransferhttp.BackupExportContext) (dbtransferhttp.BackupExportResult, error) {
			passphrase, err := settingService.GetComponentSettingSecretBytes("fixtureBackupPassphrase")
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
				AuditEvent:    "fixture_backup_manual_encrypted",
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
	if err != nil {
		t.Fatal(err)
	}
	unregisterImport, err := dbtransferhttp.RegisterBackupImportCodec("test-fixture-codec", dbtransferhttp.BackupImportCodec{
		HeaderBytes:       len(backupenvelope.Magic),
		PassphraseFields:  []string{"fixtureBackupPassphrase"},
		Match:             backupenvelope.IsEnvelope,
		FailureAuditEvent: "fixture_backup_restore_failed",
		Decode: func(ctx dbtransferhttp.BackupImportContext) ([]byte, error) {
			passphrase := ctx.Gin.PostForm("fixtureBackupPassphrase")
			if passphrase == "" {
				passphrase = ctx.Gin.PostForm("backupPassphrase")
			}
			if passphrase == "" {
				return nil, dbtransferhttp.NewBackupCodecError(http.StatusBadRequest, "decryption_failed", nil)
			}
			return backupenvelope.Open(ctx.Payload, []byte(passphrase))
		},
	})
	if err != nil {
		unregisterExport()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		unregisterImport()
		unregisterExport()
	})
}
