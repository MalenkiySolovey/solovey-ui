package sqlite

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// OpenReadOnly opens an existing file for diagnostics without initialization,
// migrations, defaults or publication as the runtime database. The caller owns
// this independent sql.DB and must close it after inspection.
func OpenReadOnly(ctx context.Context, dbPath string) (*gorm.DB, error) {
	if ctx == nil {
		return nil, fmt.Errorf("read-only database context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("diagnostic database must be an existing regular file")
	}
	location := &url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	if !strings.HasPrefix(location.Path, "/") {
		location.Path = "/" + location.Path
	}
	query := location.Query()
	query.Set("mode", "ro")
	query.Set("_query_only", "1")
	query.Set("_busy_timeout", "1000")
	location.RawQuery = query.Encode()
	opened, err := gorm.Open(sqlite.Open(location.String()), &gorm.Config{
		Logger: gormlogger.Discard, DisableAutomaticPing: true,
	})
	if err != nil {
		return nil, err
	}
	pool, err := opened.DB()
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	if err := pool.PingContext(ctx); err != nil {
		_ = pool.Close()
		return nil, err
	}
	return opened.WithContext(ctx), nil
}
