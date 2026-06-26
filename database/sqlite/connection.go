package sqlite

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	configlogging "github.com/MalenkiySolovey/solovey-ui/config/logging"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	dbMu sync.RWMutex
	db   *gorm.DB
)

const (
	dbMaxOpenConnsEnv        = "SUI_DB_MAX_OPEN_CONNS"
	dbMaxIdleConnsEnv        = "SUI_DB_MAX_IDLE_CONNS"
	defaultDBMaxOpenConns    = 8
	defaultDBMaxIdleConns    = 4
	defaultDBConnMaxLifetime = time.Hour
)

type dbPoolConfig struct {
	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime time.Duration
}

type dbPoolSetter interface {
	SetMaxOpenConns(int)
	SetMaxIdleConns(int)
	SetConnMaxLifetime(time.Duration)
}

func open(dbPath string) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return err
	}

	gormLog := gormlogger.Interface(gormlogger.Discard)
	if configlogging.IsDebug() {
		gormLog = gormlogger.Default
	}
	separator := "?"
	if strings.Contains(dbPath, "?") {
		separator = "&"
	}
	dsn := dbPath + separator + "_busy_timeout=10000&_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=on"
	openedDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: gormLog})
	if err != nil {
		return err
	}

	sqlDB, err := openedDB.DB()
	if err != nil {
		return err
	}
	applyDBPoolConfig(sqlDB, resolvedDBPoolConfig())
	if configlogging.IsDebug() {
		openedDB = openedDB.Debug()
	}

	dbMu.Lock()
	db = openedDB
	dbMu.Unlock()
	return nil
}

func resolvedDBPoolConfig() dbPoolConfig {
	maxOpen := parseDBPoolLimitEnv(dbMaxOpenConnsEnv, defaultDBMaxOpenConns, func(value int) bool { return value > 0 })
	maxIdle := parseDBPoolLimitEnv(dbMaxIdleConnsEnv, defaultDBMaxIdleConns, func(value int) bool { return value >= 0 })
	if maxIdle > maxOpen {
		maxIdle = maxOpen
	}
	return dbPoolConfig{
		maxOpenConns:    maxOpen,
		maxIdleConns:    maxIdle,
		connMaxLifetime: defaultDBConnMaxLifetime,
	}
}

func parseDBPoolLimitEnv(key string, fallback int, valid func(int) bool) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || !valid(parsed) {
		return fallback
	}
	return parsed
}

func applyDBPoolConfig(pool dbPoolSetter, cfg dbPoolConfig) {
	pool.SetMaxOpenConns(cfg.maxOpenConns)
	pool.SetMaxIdleConns(cfg.maxIdleConns)
	pool.SetConnMaxLifetime(cfg.connMaxLifetime)
}

func DB() *gorm.DB {
	dbMu.RLock()
	defer dbMu.RUnlock()
	return db
}

// Close atomically detaches and closes the active database connection.
func Close() error {
	dbMu.Lock()
	current := db
	db = nil
	dbMu.Unlock()
	if current == nil {
		return nil
	}
	sqlDB, err := current.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
