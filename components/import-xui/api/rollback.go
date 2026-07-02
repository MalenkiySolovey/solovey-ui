//go:build !minimal

package importxui

import (
	dbimport "github.com/MalenkiySolovey/solovey-ui/components/import-xui/database"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"

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
