package api

import (
	"time"

	"github.com/gin-gonic/gin"
)

func respondDatabaseBackupError(c *gin.Context, status int, errorClass string) {
	c.JSON(status, Msg{
		Success: false,
		Msg:     "backup: " + errorClass,
		Obj:     gin.H{"errorClass": errorClass},
	})
}

func writeDatabaseDownload(c *gin.Context, payload []byte, encrypted bool) {
	filename := "solovey-ui_" + time.Now().Format("20060102-150405") + ".db"
	if encrypted {
		filename += ".aes"
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	_, _ = c.Writer.Write(payload)
}
