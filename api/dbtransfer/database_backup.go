package dbtransfer

import "github.com/gin-gonic/gin"

type databaseBackupRequest struct {
	Exclude               string
	EncryptTelegramBackup bool
}

func (a *Handler) DownloadDatabase(c *gin.Context) {
	if !a.RequireScope(c, "database", "admin") {
		return
	}
	request := parseDatabaseBackupRequest(c)
	if request.EncryptTelegramBackup {
		a.getEncryptedDb(c, request)
		return
	}
	a.getPlainDb(c, request)
}

func parseDatabaseBackupRequest(c *gin.Context) databaseBackupRequest {
	return databaseBackupRequest{
		Exclude:               c.Query("exclude"),
		EncryptTelegramBackup: c.Query("encryptTelegramBackup") == "true",
	}
}
