package api

import (
	"github.com/MalenkiySolovey/solovey-ui/database"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) ImportDb(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "database", "admin") {
		return
	}
	prepared, ok := a.openDatabaseImportFile(c)
	if !ok {
		return
	}
	defer prepared.Close()

	err := database.ImportDB(prepared.file)
	a.respondDatabaseImportResult(c, err)
}
