package dbtransfer

import (
	"net/http"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/internal/integrations/telegram"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-gonic/gin"
)

func (a *Handler) getEncryptedDb(c *gin.Context, request databaseBackupRequest) {
	hasPassphrase, err := a.SettingService.HasTelegramBackupPassphrase()
	if err != nil {
		respondDatabaseBackupError(c, http.StatusInternalServerError, "settings")
		return
	}
	if !hasPassphrase {
		respondDatabaseBackupError(c, http.StatusBadRequest, "missing_passphrase")
		return
	}
	db, err := backup.Export(request.Exclude)
	if err != nil {
		a.Audit(c, a.Actor(c), "db_export_failed", "database", service.AuditSeverityWarn, map[string]any{
			"channel":   "local_download",
			"encrypted": true,
		})
		a.JSONMsg(c, "", err)
		return
	}
	payloadSize := len(db)
	defer common.WipeBytes(db)

	passphrase, err := a.SettingService.GetTelegramBackupPassphraseBytes()
	if err != nil {
		respondDatabaseBackupError(c, http.StatusInternalServerError, "settings")
		return
	}
	defer common.WipeBytes(passphrase)
	if len(passphrase) == 0 {
		respondDatabaseBackupError(c, http.StatusBadRequest, "missing_passphrase")
		return
	}

	envelope, err := integrationtelegram.BuildTelegramBackupEnvelope(db, passphrase)
	common.WipeBytes(passphrase)
	if err != nil {
		respondDatabaseBackupError(c, http.StatusInternalServerError, "encryption_failed")
		return
	}
	common.WipeBytes(db)

	a.Audit(c, a.Actor(c), "tg_backup_manual_encrypted", "database", service.AuditSeverityInfo, map[string]any{
		"channel":           "local_download",
		"payloadSizeBytes":  int64(payloadSize),
		"envelopeSizeBytes": int64(len(envelope)),
		"excludedTables":    backup.ParseExcludes(request.Exclude),
	})
	writeDatabaseDownload(c, envelope, true)
}
