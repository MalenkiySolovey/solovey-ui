package backup

import (
	"bytes"
	"io"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func validateSQLiteBackup(path string) error {
	probe, err := gorm.Open(gormsqlite.Open(sqliteReadOnlyDSN(path)), &gorm.Config{Logger: gormlogger.Discard})
	if err != nil {
		return common.NewErrorf("Error checking db: %v", err)
	}
	if sqlDB, dbErr := probe.DB(); dbErr == nil {
		defer sqlDB.Close()
	}
	var result string
	if err := probe.Raw("PRAGMA integrity_check").Scan(&result).Error; err != nil {
		return common.NewErrorf("Error checking db integrity: %v", err)
	}
	if result != "ok" {
		return common.NewErrorf("Invalid db integrity: %s", result)
	}
	if isXUIDatabase(probe) {
		return common.NewError("this looks like a 3x-ui/x-ui database, not an s-ui backup; use \"Migrate from 3x-ui\" to import it instead of Restore")
	}
	return validateVersionedBackupConfig(probe)
}

func isXUIDatabase(probe *gorm.DB) bool {
	for _, table := range []string{"client_traffics", "inbound_client_ips"} {
		if probe.Migrator().HasTable(table) {
			return true
		}
	}
	return false
}

func validateVersionedBackupConfig(probe *gorm.DB) error {
	if !probe.Migrator().HasTable(&model.Setting{}) {
		return nil
	}
	var version string
	if err := probe.Model(&model.Setting{}).Select("value").Where("key = ?", "version").Scan(&version).Error; err != nil {
		return common.NewErrorf("Error checking db settings: %v", err)
	}
	if strings.TrimSpace(version) == "" {
		return nil
	}
	var configRows int64
	if err := probe.Model(&model.Setting{}).Where("key = ?", "config").Count(&configRows).Error; err != nil {
		return common.NewErrorf("Error checking db config: %v", err)
	}
	if configRows == 0 {
		logger.Warning("versioned S-UI backup is missing settings.config; legacy or partial backup, restore will continue")
	}
	return nil
}

func sqliteReadOnlyDSN(path string) string {
	urlPath := filepath.ToSlash(path)
	if runtime.GOOS == "windows" && !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	u := url.URL{Scheme: "file", Path: urlPath}
	values := url.Values{}
	values.Set("mode", "ro")
	u.RawQuery = values.Encode()
	return u.String()
}

func IsSQLite(file io.Reader) (bool, error) {
	signature := []byte("SQLite format 3\x00")
	buf := make([]byte, len(signature))
	if _, err := file.Read(buf); err != nil {
		return false, err
	}
	return bytes.Equal(buf, signature), nil
}
