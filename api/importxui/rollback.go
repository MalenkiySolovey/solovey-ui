package importxui

import (
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbimport "github.com/MalenkiySolovey/solovey-ui/database/importxui"

	"github.com/gin-gonic/gin"
)

func xuiRollbackBackupPath(c *gin.Context) string {
	backupPath := c.PostForm("backup")
	if backupPath == "" {
		backupPath = c.Query("backup")
	}
	return backupPath
}

func validateRollbackPath(path string) error {
	return dbimport.ValidateRollbackBackupPath(path, configstorage.GetDBPath())
}
