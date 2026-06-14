package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/config"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	dbMu sync.RWMutex
	db   *gorm.DB
)
var adaptToCurrentVersion = AdaptToCurrentVersion

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

func initUser(dbPath string) error {
	var count int64
	err := db.Model(&model.User{}).Count(&count).Error
	if err != nil {
		return err
	}
	passwordPath := initialAdminPasswordPath(dbPath)
	if count == 0 {
		password := common.Random(24)
		passwordHash, err := common.HashPassword(password)
		if err != nil {
			return err
		}
		if err := writeInitialAdminPassword(passwordPath, password); err != nil {
			return err
		}
		user := &model.User{
			Username: "admin",
			Password: passwordHash,
		}
		if err := db.Create(user).Error; err != nil {
			_ = os.Remove(passwordPath)
			return err
		}
		notifyInitialAdminPasswordSaved(passwordPath)
		return nil
	}
	warnIfInitialAdminPasswordFileExists(passwordPath)
	return nil
}

func OpenDB(dbPath string) error {
	dir := filepath.Dir(dbPath)
	err := os.MkdirAll(dir, 0o750)
	if err != nil {
		return err
	}

	var gormLog gormlogger.Interface

	if config.IsDebug() {
		gormLog = gormlogger.Default
	} else {
		gormLog = gormlogger.Discard
	}

	c := &gorm.Config{
		Logger: gormLog,
	}
	sep := "?"
	if strings.Contains(dbPath, "?") {
		sep = "&"
	}
	dsn := dbPath + sep + "_busy_timeout=10000&_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=on"
	openedDB, err := gorm.Open(sqlite.Open(dsn), c)
	if err != nil {
		return err
	}

	sqlDB, err := openedDB.DB()
	if err != nil {
		return err
	}
	// SQLite is a single-writer database. Allowing many concurrent open
	// connections only spreads writers across them and produces SQLITE_BUSY
	// errors during stats inserts. Keep a small read pool plus one effective
	// writer driven through `_busy_timeout` to serialize gracefully.
	applyDBPoolConfig(sqlDB, resolvedDBPoolConfig())

	if config.IsDebug() {
		openedDB = openedDB.Debug()
	}
	dbMu.Lock()
	db = openedDB
	dbMu.Unlock()
	return nil
}

func resolvedDBPoolConfig() dbPoolConfig {
	maxOpen := parseDBPoolLimitEnv(dbMaxOpenConnsEnv, defaultDBMaxOpenConns, func(value int) bool {
		return value > 0
	})
	maxIdle := parseDBPoolLimitEnv(dbMaxIdleConnsEnv, defaultDBMaxIdleConns, func(value int) bool {
		return value >= 0
	})
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

func InitDB(dbPath string) error {
	err := OpenDB(dbPath)
	if err != nil {
		return err
	}

	// Default Outbounds
	if err := ensureDefaultOutbound(gormDefaultOutboundStore{db: db}); err != nil {
		return err
	}

	err = db.AutoMigrate(
		&model.Setting{},
		&model.Tls{},
		&model.Inbound{},
		&model.Outbound{},
		&model.RemoteOutboundSubscription{},
		&model.RemoteOutboundGroup{},
		&model.RemoteOutboundGroupConnection{},
		&model.RemoteOutboundConnection{},
		&model.Service{},
		&model.Endpoint{},
		&model.User{},
		&model.Tokens{},
		&model.Stats{},
		&model.ClientIP{},
		&model.Client{},
		&model.Changes{},
		&model.AuditEvent{},
	)
	if err != nil {
		return err
	}
	if err := dropDeprecatedTables(); err != nil {
		return err
	}
	if err := ensureNoTLSRow(); err != nil {
		return err
	}
	if err := ensureIndexes(); err != nil {
		return err
	}
	err = initUser(dbPath)
	if err != nil {
		return err
	}
	// Post-migration adaptation is part of startup safety: indexes, legacy
	// password rehashes and the settings.version pointer must be current
	// before the panel serves traffic.
	if err := adaptToCurrentVersion(); err != nil {
		return fmt.Errorf("post-migration adapt failed: %w", err)
	}
	if err := ensureSortOrders(); err != nil {
		return fmt.Errorf("sort-order backfill failed: %w", err)
	}

	return nil
}

type defaultOutboundStore interface {
	HasTable(value any) bool
	CreateTable(values ...any) error
	Create(value any) error
}

type gormDefaultOutboundStore struct {
	db *gorm.DB
}

func (s gormDefaultOutboundStore) HasTable(value any) bool {
	return s.db.Migrator().HasTable(value)
}

func (s gormDefaultOutboundStore) CreateTable(values ...any) error {
	return s.db.Migrator().CreateTable(values...)
}

func (s gormDefaultOutboundStore) Create(value any) error {
	return s.db.Create(value).Error
}

func ensureDefaultOutbound(store defaultOutboundStore) error {
	if store.HasTable(&model.Outbound{}) {
		return nil
	}
	if err := store.CreateTable(&model.Outbound{}); err != nil {
		return err
	}
	defaultOutbound := []model.Outbound{
		{Type: "direct", Tag: "direct", Options: json.RawMessage(`{}`)},
	}
	return store.Create(&defaultOutbound)
}

// dropDeprecatedTables removes tables that backed features which have since been
// retired, so a re-saved database does not keep dead schema around. The 3x-ui
// scheduled-sync feature (xui_sync_profiles) and its SSH host-key store
// (xui_known_hosts) were removed in favour of the one-shot .db upload importer;
// these tables are dropped on every startup. DROP TABLE IF EXISTS is idempotent
// and a no-op once the tables are gone.
func dropDeprecatedTables() error {
	statements := []string{
		"DROP TABLE IF EXISTS xui_sync_profiles",
		"DROP TABLE IF EXISTS xui_known_hosts",
	}
	for _, query := range statements {
		if err := db.Exec(query).Error; err != nil {
			return err
		}
	}
	return nil
}

func ensureNoTLSRow() error {
	return ensureNoTLSRowOn(db)
}

func ensureNoTLSRowOn(target *gorm.DB) error {
	if target == nil {
		return nil
	}
	return target.Exec("INSERT OR IGNORE INTO tls(id, name, server, client) VALUES(0, ?, ?, ?)", "__none__", []byte("{}"), []byte("{}")).Error
}

func ensureIndexes() error {
	obsoleteIndexes := []string{
		"DROP INDEX IF EXISTS idx_client_ips_client_ip",
	}
	for _, query := range obsoleteIndexes {
		if err := db.Exec(query).Error; err != nil {
			return err
		}
	}
	// settings.key must be unique, but the historical read-then-create path could
	// race two first-start inserts into duplicate rows. Drop any duplicates
	// (keeping the lowest id, which carries the original default) before enforcing
	// uniqueness, so the unique index below also closes that TOCTOU gap and makes
	// every keyed settings read an index lookup instead of a full table scan (P-a).
	if err := db.Exec("DELETE FROM settings WHERE id NOT IN (SELECT MIN(id) FROM settings GROUP BY key)").Error; err != nil {
		return err
	}
	indexes := []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_settings_key ON settings(key)",
		"CREATE INDEX IF NOT EXISTS idx_stats_lookup ON stats(date_time, resource, tag)",
		"CREATE INDEX IF NOT EXISTS idx_changes_lookup ON changes(date_time, actor, key)",
		"CREATE INDEX IF NOT EXISTS idx_audit_events_lookup ON audit_events(date_time, actor, event)",
		"CREATE INDEX IF NOT EXISTS idx_audit_events_event_dt ON audit_events(event, date_time DESC)",
		"CREATE INDEX IF NOT EXISTS idx_audit_events_severity_dt ON audit_events(severity, date_time DESC)",
		"CREATE INDEX IF NOT EXISTS idx_clients_name ON clients(name)",
		"CREATE INDEX IF NOT EXISTS idx_clients_sub_secret ON clients(sub_secret)",
		"CREATE INDEX IF NOT EXISTS idx_clients_sort_order ON clients(sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_inbounds_sort_order ON inbounds(sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_outbounds_sort_order ON outbounds(sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_subscriptions_sort_order ON remote_outbound_subscriptions(sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_groups_subscription_sort_order ON remote_outbound_groups(subscription_id, sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_group_connections_group ON remote_outbound_group_connections(group_id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_group_connections_connection ON remote_outbound_group_connections(connection_id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_connections_subscription_sort_order ON remote_outbound_connections(subscription_id, sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_connections_group_sort_order ON remote_outbound_connections(group_id, sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_remote_outbound_connections_outbound_id ON remote_outbound_connections(outbound_id)",
		"CREATE INDEX IF NOT EXISTS idx_endpoints_sort_order ON endpoints(sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_services_sort_order ON services(sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_tls_sort_order ON tls(sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_users_sort_order ON users(sort_order, id)",
		"CREATE INDEX IF NOT EXISTS idx_client_ips_client_legacy_ip ON client_ips(client_name, ip) WHERE ip IS NOT NULL AND ip != ''",
		"CREATE INDEX IF NOT EXISTS idx_client_ips_last_seen ON client_ips(last_seen)",
	}
	for _, query := range indexes {
		if err := db.Exec(query).Error; err != nil {
			return err
		}
	}
	return nil
}

func ensureSortOrders() error {
	for _, table := range []string{"inbounds", "clients", "outbounds", "remote_outbound_subscriptions", "remote_outbound_groups", "remote_outbound_connections", "endpoints", "services", "tls", "users"} {
		if err := ensureTableSortOrder(table); err != nil {
			return err
		}
	}
	return nil
}

func ensureTableSortOrder(table string) error {
	if !db.Migrator().HasTable(table) || !db.Migrator().HasColumn(table, "sort_order") {
		return nil
	}

	quotedTable := quoteSQLiteIdentifier(table)
	rows := []struct {
		ID        int64
		SortOrder int
	}{}
	if err := db.Raw(fmt.Sprintf("SELECT id, sort_order FROM %s ORDER BY sort_order ASC, id ASC", quotedTable)).Scan(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	needsBackfill := false
	for index, row := range rows {
		if row.SortOrder != index+1 {
			needsBackfill = true
			break
		}
	}
	if !needsBackfill {
		return nil
	}

	return db.Transaction(func(tx *gorm.DB) error {
		query := fmt.Sprintf("UPDATE %s SET sort_order = ? WHERE id = ?", quotedTable)
		for index, row := range rows {
			if err := tx.Exec(query, index+1, row.ID).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func quoteSQLiteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func GetDB() *gorm.DB {
	dbMu.RLock()
	defer dbMu.RUnlock()
	return db
}

func IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
