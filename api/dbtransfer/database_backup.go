package dbtransfer

import "github.com/gin-gonic/gin"

type databaseBackupRequest struct {
	Exclude string
}

func (a *Handler) DownloadDatabase(c *gin.Context) {
	if !a.RequireScope(c, "database", "admin") {
		return
	}
	request := parseDatabaseBackupRequest(c)
	if codec, ok := selectedBackupExportCodec(c); ok {
		a.getEncodedDb(c, request, codec)
		return
	}
	if backupExportCodecRequested(c) {
		respondDatabaseBackupError(c, 400, "unsupported_encryption")
		return
	}
	a.getPlainDb(c, request)
}

func parseDatabaseBackupRequest(c *gin.Context) databaseBackupRequest {
	return databaseBackupRequest{
		Exclude: c.Query("exclude"),
	}
}
