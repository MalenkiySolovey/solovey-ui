package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/MalenkiySolovey/solovey-ui/config/identity"
)

func GetDBFolderPath() string {
	dbFolderPath := os.Getenv("SUI_DB_FOLDER")
	if dbFolderPath == "" {
		dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
		if err != nil {
			// Cross-platform fallback path
			if runtime.GOOS == "windows" {
				return "C:\\Program Files\\solovey-ui\\db"
			}
			return "/usr/local/solovey-ui/db"
		}
		dbFolderPath = filepath.Join(dir, "db")
	}
	return dbFolderPath
}

func GetDBPath() string {
	return filepath.Join(GetDBFolderPath(), fmt.Sprintf("%s.db", identity.GetName()))
}

// CachePath and StagingPath consume optional deployment path facts. Existing
// deployments retain their paths; these functions assign no platform semantics.
func CachePath(relative string) string {
	root := os.Getenv("SUI_CACHE_FOLDER")
	if root == "" {
		root = GetDBFolderPath()
	}
	return filepath.Join(root, relative)
}

func StagingPath(relative string) string {
	root := os.Getenv("SUI_STAGING_FOLDER")
	if root == "" {
		root = GetDBFolderPath()
	}
	return filepath.Join(root, relative)
}
