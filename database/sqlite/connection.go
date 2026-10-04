package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

func prepareForInit(ctx context.Context) error {
	dbMu.RLock()
	current, generation := db, activeGeneration
	owner := maintenance
	contextOwner := maintenanceFrom(ctx)
	expiredContext := contextOwner != nil && (contextOwner != owner || contextOwner.expired)
	dbMu.RUnlock()
	if expiredContext {
		return ErrRetired
	}
	if owner != nil && !IsMaintenanceContext(ctx) {
		return ErrMaintenance
	}
	if current == nil {
		return finishRetiredGeneration(ctx, generation)
	}
	pool, err := current.DB()
	if err != nil {
		return fmt.Errorf("inspect active database: %w", err)
	}
	if err := pool.PingContext(ctx); err == nil {
		return errors.New("database is already initialized")
	} else if !isClosedPoolError(err) {
		return fmt.Errorf("check active database: %w", err)
	}
	// Compatibility with legacy callers that closed the exposed sql.DB
	// directly. Detach only a handle proven permanently closed.
	dbMu.Lock()
	if db == current {
		db, activeDBPath = nil, ""
		if generation != nil {
			generation.phase = generationRetired
		}
		changedLocked()
	}
	dbMu.Unlock()
	return finishRetiredGeneration(ctx, generation)
}

func isClosedPoolError(err error) bool {
	// database/sql does not export its dbClosed sentinel. Drivers may return
	// either sql.ErrConnDone or the stable database/sql sentinel text after a
	// caller closes an exposed *sql.DB directly.
	return errors.Is(err, sql.ErrConnDone) || err.Error() == "sql: database is closed"
}

var (
	dbMu         sync.RWMutex
	db           *gorm.DB
	activeDBPath string
	initMu       sync.Mutex
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
	return openContext(context.Background(), dbPath)
}

func openContext(ctx context.Context, dbPath string) error {
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
	generation := &generation{cfg: resolvedDBPoolConfig(), initializing: true}
	dbMu.RLock()
	owner := maintenance
	dbMu.RUnlock()
	if owner != nil {
		if !IsMaintenanceContext(ctx) {
			return ErrMaintenance
		}
		generation.phase = generationPrivate
	}
	sqlDB := sql.OpenDB(&generationConnector{dsn: dsn, generation: generation})
	generation.pool = sqlDB
	openedDB, err := gorm.Open(sqlite.New(sqlite.Config{DSN: dsn, Conn: sqlDB}), &gorm.Config{Logger: gormLog})
	if err != nil {
		_ = sqlDB.Close()
		return err
	}
	applyDBPoolConfig(sqlDB, generation.cfg)
	if owner != nil {
		sqlDB.SetMaxIdleConns(0)
	}
	if configlogging.IsDebug() {
		openedDB = openedDB.Debug()
	}

	dbMu.Lock()
	generation.initializing = false
	db = openedDB
	activeDBPath = dbPath
	activeGeneration = generation
	changedLocked()
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
	if db == nil && maintenance != nil {
		return maintenance.gap
	}
	return db
}

func currentDatabasePath() string {
	dbMu.RLock()
	defer dbMu.RUnlock()
	return activeDBPath
}

// Close retires the active generation. A successful close means its actual
// connections and finite operations have ended, including borrowed sql.Conn.
func Close() error {
	return CloseContext(context.Background())
}

// CloseContext preserves a private restore scope when the owner closes a
// rejected candidate. Terminal shutdown expires that scope as well.
func CloseContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("sqlite close context is required")
	}
	dbMu.Lock()
	if owner := maintenanceFrom(ctx); owner != nil && (owner != maintenance || owner.expired) {
		dbMu.Unlock()
		return ErrRetired
	}
	current, generation, owner := db, activeGeneration, maintenance
	db = nil
	activeDBPath = ""
	if generation != nil {
		generation.phase = generationRetired
	}
	changedLocked()
	dbMu.Unlock()
	var closeErr error
	if current != nil {
		sqlDB, err := current.DB()
		if err != nil {
			closeErr = err
		} else {
			closeErr = sqlDB.Close()
		}
	}
	if owner != nil && maintenanceFrom(ctx) != owner {
		owner.End()
	}
	return errors.Join(closeErr, finishRetiredGeneration(ctx, generation))
}

// CloseForFileSwap freezes admissions, drains actual connection lifetimes,
// proves a non-busy TRUNCATE checkpoint and permanently retires the pool.
// Cancellation/checkpoint failure restores the original still-live generation.
func CloseForFileSwap(ctx context.Context) error {
	if ctx == nil {
		return errors.New("sqlite file-swap context is required")
	}
	if !IsMaintenanceContext(ctx) {
		owner, err := BeginMaintenance(ctx)
		if err != nil {
			return err
		}
		defer owner.End()
		ctx = owner.Context(ctx)
	}
	drainCtx, cancel := context.WithTimeout(ctx, maintenanceDrainTimeout)
	defer cancel()
	dbMu.Lock()
	current, generation := db, activeGeneration
	if current == nil {
		dbMu.Unlock()
		return errors.New("sqlite database is unavailable for file swap")
	}
	previousPhase := generation.phase
	generation.phase = generationDraining
	changedLocked()
	dbMu.Unlock()
	sqlDB, err := current.DB()
	if err != nil {
		resumeGeneration(generation, previousPhase)
		return err
	}
	// With idle connections removed, database/sql closes each driver connection
	// only after its supported Rows/Tx/Conn/Raw resources have released it.
	sqlDB.SetMaxIdleConns(0)
	if err := waitGeneration(drainCtx, generation, true); err != nil {
		resumeGeneration(generation, previousPhase)
		return fmt.Errorf("drain sqlite connections before file swap: %w", err)
	}
	var busy, logFrames, checkpointed int
	if err := sqlDB.QueryRowContext(drainCtx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed); err != nil {
		resumeGeneration(generation, previousPhase)
		return fmt.Errorf("sqlite WAL checkpoint failed before file swap: %w", err)
	}
	if busy != 0 {
		resumeGeneration(generation, previousPhase)
		return fmt.Errorf("sqlite WAL checkpoint is busy before file swap: busy=%d log=%d checkpointed=%d",
			busy, logFrames, checkpointed)
	}
	for {
		if err := waitGeneration(drainCtx, generation, true); err != nil {
			resumeGeneration(generation, previousPhase)
			return err
		}
		dbMu.Lock()
		if generation.connections != 0 {
			dbMu.Unlock()
			continue
		}
		generation.phase = generationRetired
		db, activeDBPath = nil, ""
		changedLocked()
		dbMu.Unlock()
		break
	}
	return errors.Join(sqlDB.Close(), finishRetiredGeneration(drainCtx, generation))
}

func resumeGeneration(generation *generation, phase generationPhase) {
	if phase == generationPrivate {
		generation.pool.SetMaxIdleConns(0)
	} else {
		applyDBPoolConfig(generation.pool, generation.cfg)
	}
	dbMu.Lock()
	defer dbMu.Unlock()
	if activeGeneration == generation && generation.phase != generationRetired {
		generation.phase = phase
		changedLocked()
	}
}

func finishRetiredGeneration(ctx context.Context, generation *generation) error {
	if generation == nil {
		return nil
	}
	drainCtx, cancel := context.WithTimeout(ctx, maintenanceDrainTimeout)
	defer cancel()
	if err := waitGeneration(drainCtx, generation, true); err != nil {
		return err
	}
	if err := waitGeneration(drainCtx, generation, false); err != nil {
		return err
	}
	dbMu.Lock()
	defer dbMu.Unlock()
	if activeGeneration == generation && generation.phase == generationRetired {
		activeGeneration = nil
		changedLocked()
	}
	return nil
}

func IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
