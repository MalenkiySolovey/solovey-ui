package api

import (
	"net/http"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) getEncryptedDb(c *gin.Context, request databaseBackupRequest) {
	hasPassphrase, err := a.SettingService.HasTelegramBackupPassphrase()
	if err != nil {
		respondDatabaseBackupError(c, http.StatusInternalServerError, "settings")
		return
	}
	if !hasPassphrase {
		respondDatabaseBackupError(c, http.StatusBadRequest, "missing_passphrase")
		return
	}
	db, err := database.GetDb(request.Exclude)
	if err != nil {
		a.recordAudit(c, requestActor(c), "db_export_failed", "database", service.AuditSeverityWarn, map[string]any{
			"channel":   "local_download",
			"encrypted": true,
		})
		jsonMsg(c, "", err)
		return
	}
	payloadSize := len(db)
	defer wipeBytes(db)

	passphrase, err := a.SettingService.GetTelegramBackupPassphraseBytes()
	if err != nil {
		respondDatabaseBackupError(c, http.StatusInternalServerError, "settings")
		return
	}
	defer wipeBytes(passphrase)
	if len(passphrase) == 0 {
		respondDatabaseBackupError(c, http.StatusBadRequest, "missing_passphrase")
		return
	}

	envelope, err := service.BuildTelegramBackupEnvelope(db, passphrase)
	wipeBytes(passphrase)
	if err != nil {
		respondDatabaseBackupError(c, http.StatusInternalServerError, "encryption_failed")
		return
	}
	wipeBytes(db)

	a.recordAudit(c, requestActor(c), "tg_backup_manual_encrypted", "database", service.AuditSeverityInfo, map[string]any{
		"channel":           "local_download",
		"payloadSizeBytes":  int64(payloadSize),
		"envelopeSizeBytes": int64(len(envelope)),
		"excludedTables":    database.ParseBackupExcludes(request.Exclude),
	})
	writeDatabaseDownload(c, envelope, true)
}
