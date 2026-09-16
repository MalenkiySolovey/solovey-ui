package storage

import (
	"path/filepath"
	"testing"
)

func TestOptionalWorkPathsPreserveDefaultAndSeparateDurableState(t *testing.T) {
	db, cache, staging := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("SUI_DB_FOLDER", db)
	t.Setenv("SUI_CACHE_FOLDER", "")
	t.Setenv("SUI_STAGING_FOLDER", "")
	if CachePath("owner") != filepath.Join(db, "owner") || StagingPath("owner") != filepath.Join(db, "owner") {
		t.Fatal("existing paths changed")
	}
	t.Setenv("SUI_CACHE_FOLDER", cache)
	t.Setenv("SUI_STAGING_FOLDER", staging)
	if CachePath("owner") != filepath.Join(cache, "owner") || StagingPath("owner") != filepath.Join(staging, "owner") || GetDBFolderPath() != db {
		t.Fatal("path owners overlap")
	}
}
