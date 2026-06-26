package dbtransfer

import (
	"github.com/MalenkiySolovey/solovey-ui/database/backup"

	"github.com/gin-gonic/gin"
)

func (a *Handler) ImportDb(c *gin.Context) {
	if !a.RequireScope(c, "database", "admin") {
		return
	}
	prepared, ok := a.openDatabaseImportFile(c)
	if !ok {
		return
	}
	defer prepared.Close()

	err := backup.Restore(prepared.MultipartFile())
	a.respondDatabaseImportResult(c, err)
}
