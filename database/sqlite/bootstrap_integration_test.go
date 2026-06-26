package sqlite

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type mockDefaultOutboundStore struct {
	hasTable       bool
	createTableErr error
	createErr      error
	createTable    int
	create         int
}

func (s *mockDefaultOutboundStore) HasTable(any) bool {
	return s.hasTable
}

func (s *mockDefaultOutboundStore) CreateTable(...any) error {
	s.createTable++
	return s.createTableErr
}

func (s *mockDefaultOutboundStore) Create(any) error {
	s.create++
	return s.createErr
}

func TestEnsureDefaultOutboundReturnsCreateTableError(t *testing.T) {
	want := errors.New("create table failed")
	store := &mockDefaultOutboundStore{createTableErr: want}

	err := ensureDefaultOutbound(store)
	if !errors.Is(err, want) {
		t.Fatalf("expected CreateTable error, got %v", err)
	}
	if store.create != 0 {
		t.Fatal("default outbound row should not be created after CreateTable failure")
	}
}

func TestEnsureDefaultOutboundReturnsCreateError(t *testing.T) {
	want := errors.New("create default outbound failed")
	store := &mockDefaultOutboundStore{createErr: want}

	err := ensureDefaultOutbound(store)
	if !errors.Is(err, want) {
		t.Fatalf("expected Create error, got %v", err)
	}
	if store.createTable != 1 || store.create != 1 {
		t.Fatalf("unexpected call counts: createTable=%d create=%d", store.createTable, store.create)
	}
}

func TestEnsureDefaultOutboundSkipsExistingTable(t *testing.T) {
	store := &mockDefaultOutboundStore{hasTable: true}

	if err := ensureDefaultOutbound(store); err != nil {
		t.Fatal(err)
	}
	if store.createTable != 0 || store.create != 0 {
		t.Fatalf("existing table should skip writes: createTable=%d create=%d", store.createTable, store.create)
	}
}

func TestInitDropsObsoleteClientIPUniqueIndex(t *testing.T) {
	dbDir := makeDBTempDir(t, "s-ui-db-test-")
	dbPath := filepath.Join(dbDir, "s-ui.db")
	legacy, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := legacy.Exec(`
CREATE TABLE client_ips (
	id integer PRIMARY KEY AUTOINCREMENT,
	client_name text,
	ip text,
	ip_hash text,
	ip_display text,
	first_seen integer,
	last_seen integer
)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := legacy.Exec("CREATE UNIQUE INDEX idx_client_ips_client_ip ON client_ips(client_name, ip)").Error; err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := legacy.DB(); err == nil {
		_ = sqlDB.Close()
	}

	if err := Init(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeMainDB(t)
		cleanupSQLiteSidecars(dbPath)
	})

	hasIndex, err := dbTestHasIndex(DB(), "client_ips", "idx_client_ips_client_ip")
	if err != nil {
		t.Fatal(err)
	}
	if hasIndex {
		t.Fatal("obsolete client/ip unique index was not dropped")
	}
	if err := DB().Exec(`
INSERT INTO client_ips(client_name, ip, ip_hash, first_seen, last_seen)
VALUES('alice', '', 'hash-1', 1, 1), ('alice', '', 'hash-2', 2, 2)
`).Error; err != nil {
		t.Fatalf("multiple empty legacy ip rows should be allowed after sqlite.Init: %v", err)
	}
}

func TestInitCreatesStatsDashboardIndex(t *testing.T) {
	dbDir := makeDBTempDir(t, "s-ui-db-test-")
	dbPath := filepath.Join(dbDir, "s-ui.db")
	if err := Init(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeMainDB(t)
		cleanupSQLiteSidecars(dbPath)
	})

	hasIndex, err := dbTestHasIndex(DB(), "stats", "idx_stats_resource_tag_dt")
	if err != nil {
		t.Fatal(err)
	}
	if !hasIndex {
		t.Fatal("stats dashboard index was not created")
	}
	hasIndex, err = dbTestHasIndex(DB(), "stats", "idx_stats_resource_dt")
	if err != nil {
		t.Fatal(err)
	}
	if !hasIndex {
		t.Fatal("stats traffic summary index was not created")
	}
}

func TestIssue14InitReturnsAdaptError(t *testing.T) {
	dbDir := makeDBTempDir(t, "s-ui-db-test-")
	dbPath := filepath.Join(dbDir, "s-ui.db")
	sentinel := errors.New("issue14 adapt failure")

	previousAdapt := adaptToCurrentVersion
	adaptToCurrentVersion = func() error {
		return sentinel
	}
	t.Cleanup(func() {
		adaptToCurrentVersion = previousAdapt
		closeMainDB(t)
		cleanupSQLiteSidecars(dbPath)
	})

	err := Init(dbPath)
	if err != nil && strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
		t.Skip(err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("sqlite.Init error = %v, want sentinel adapt error", err)
	}
	if !strings.Contains(err.Error(), "post-migration adapt failed") {
		t.Fatalf("sqlite.Init error = %q, want post-migration adapt context", err)
	}
}

func TestIssue13InitCreatesForcePasswordResetDefaultFalse(t *testing.T) {
	dbDir := makeDBTempDir(t, "s-ui-db-test-")
	dbPath := filepath.Join(dbDir, "s-ui.db")
	if err := Init(dbPath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeMainDB(t)
		cleanupSQLiteSidecars(dbPath)
	})

	if !DB().Migrator().HasColumn(&model.User{}, "force_password_reset") {
		t.Fatal("users.force_password_reset column was not created")
	}
	var admin model.User
	if err := DB().Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if admin.ForcePasswordReset {
		t.Fatalf("initial admin should not require reset: %#v", admin)
	}
}

func TestInitBackfillsSortOrderColumns(t *testing.T) {
	dbDir := makeDBTempDir(t, "s-ui-db-test-")
	dbPath := filepath.Join(dbDir, "s-ui.db")
	if err := Init(dbPath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeMainDB(t)
		cleanupSQLiteSidecars(dbPath)
	})

	if err := DB().Create(&model.Outbound{Type: "direct", Tag: "second", Options: []byte("{}")}).Error; err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"UPDATE outbounds SET sort_order = 0",
		"UPDATE tls SET sort_order = 0",
		"UPDATE users SET sort_order = 0",
	} {
		if err := DB().Exec(query).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := ensureSortOrders(); err != nil {
		t.Fatal(err)
	}

	assertSortOrders := func(table string, want []int) {
		t.Helper()
		var got []int
		if err := DB().Raw("SELECT sort_order FROM " + table + " ORDER BY sort_order ASC, id ASC").Scan(&got).Error; err != nil {
			t.Fatal(err)
		}
		if len(got) != len(want) {
			t.Fatalf("%s sort_order len=%d, want %d: %v", table, len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s sort_order=%v, want %v", table, got, want)
			}
		}
	}

	assertSortOrders("outbounds", []int{1, 2})
	assertSortOrders("tls", []int{1})
	assertSortOrders("users", []int{1})
}

func TestIssue15DBPoolConfigFromEnv(t *testing.T) {
	cases := []struct {
		name        string
		maxOpenRaw  *string
		maxIdleRaw  *string
		wantMaxOpen int
		wantMaxIdle int
	}{
		{
			name:        "unset keeps defaults",
			wantMaxOpen: defaultDBMaxOpenConns,
			wantMaxIdle: defaultDBMaxIdleConns,
		},
		{
			name:        "empty keeps defaults",
			maxOpenRaw:  stringPtr(""),
			maxIdleRaw:  stringPtr(""),
			wantMaxOpen: defaultDBMaxOpenConns,
			wantMaxIdle: defaultDBMaxIdleConns,
		},
		{
			name:        "invalid keeps defaults",
			maxOpenRaw:  stringPtr("many"),
			maxIdleRaw:  stringPtr("few"),
			wantMaxOpen: defaultDBMaxOpenConns,
			wantMaxIdle: defaultDBMaxIdleConns,
		},
		{
			name:        "nonpositive open keeps default",
			maxOpenRaw:  stringPtr("0"),
			maxIdleRaw:  stringPtr("3"),
			wantMaxOpen: defaultDBMaxOpenConns,
			wantMaxIdle: 3,
		},
		{
			name:        "negative idle keeps default",
			maxOpenRaw:  stringPtr("6"),
			maxIdleRaw:  stringPtr("-1"),
			wantMaxOpen: 6,
			wantMaxIdle: defaultDBMaxIdleConns,
		},
		{
			name:        "valid values are used",
			maxOpenRaw:  stringPtr("3"),
			maxIdleRaw:  stringPtr("2"),
			wantMaxOpen: 3,
			wantMaxIdle: 2,
		},
		{
			name:        "zero idle is accepted",
			maxOpenRaw:  stringPtr("3"),
			maxIdleRaw:  stringPtr("0"),
			wantMaxOpen: 3,
			wantMaxIdle: 0,
		},
		{
			name:        "idle above open clamps to open",
			maxOpenRaw:  stringPtr("2"),
			maxIdleRaw:  stringPtr("7"),
			wantMaxOpen: 2,
			wantMaxIdle: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setIssue15DBPoolEnv(t, tc.maxOpenRaw, tc.maxIdleRaw)

			got := resolvedDBPoolConfig()
			if got.maxOpenConns != tc.wantMaxOpen {
				t.Fatalf("maxOpenConns=%d, want %d", got.maxOpenConns, tc.wantMaxOpen)
			}
			if got.maxIdleConns != tc.wantMaxIdle {
				t.Fatalf("maxIdleConns=%d, want %d", got.maxIdleConns, tc.wantMaxIdle)
			}
			if got.connMaxLifetime != defaultDBConnMaxLifetime {
				t.Fatalf("connMaxLifetime=%s, want %s", got.connMaxLifetime, defaultDBConnMaxLifetime)
			}
		})
	}
}

func TestIssue15ApplyDBPoolConfig(t *testing.T) {
	pool := &issue15PoolSetter{}
	applyDBPoolConfig(pool, dbPoolConfig{
		maxOpenConns:    5,
		maxIdleConns:    2,
		connMaxLifetime: 30 * time.Minute,
	})

	if pool.maxOpenConns != 5 {
		t.Fatalf("SetMaxOpenConns got %d, want 5", pool.maxOpenConns)
	}
	if pool.maxIdleConns != 2 {
		t.Fatalf("SetMaxIdleConns got %d, want 2", pool.maxIdleConns)
	}
	if pool.connMaxLifetime != 30*time.Minute {
		t.Fatalf("SetConnMaxLifetime got %s, want 30m", pool.connMaxLifetime)
	}
}

func TestIssue15OpenDBUsesConfiguredMaxOpenConns(t *testing.T) {
	setIssue15DBPoolEnv(t, stringPtr("2"), stringPtr("1"))

	dbDir := makeDBTempDir(t, "s-ui-db-test-")
	dbPath := filepath.Join(dbDir, "s-ui.db")
	if err := open(dbPath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeMainDB(t)
		cleanupSQLiteSidecars(dbPath)
	})

	sqlDB, err := DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if got := sqlDB.Stats().MaxOpenConnections; got != 2 {
		t.Fatalf("MaxOpenConnections=%d, want 2", got)
	}
}

func TestOpenDBEnablesSQLiteForeignKeys(t *testing.T) {
	dbDir := makeDBTempDir(t, "s-ui-db-test-")
	dbPath := filepath.Join(dbDir, "s-ui.db")
	if err := open(dbPath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeMainDB(t)
		cleanupSQLiteSidecars(dbPath)
	})

	var enabled int
	if err := DB().Raw("PRAGMA foreign_keys").Scan(&enabled).Error; err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("PRAGMA foreign_keys=%d, want 1", enabled)
	}
}

func TestOpenDBUsesNormalSynchronousMode(t *testing.T) {
	dbDir := makeDBTempDir(t, "s-ui-db-test-")
	dbPath := filepath.Join(dbDir, "s-ui.db")
	if err := open(dbPath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeMainDB(t)
		cleanupSQLiteSidecars(dbPath)
	})

	var synchronous int
	if err := DB().Raw("PRAGMA synchronous").Scan(&synchronous).Error; err != nil {
		t.Fatal(err)
	}
	if synchronous != 1 {
		t.Fatalf("PRAGMA synchronous=%d, want NORMAL(1)", synchronous)
	}
}

func TestInitAllowsNoTLSInboundWithForeignKeys(t *testing.T) {
	dbDir := makeDBTempDir(t, "s-ui-db-test-")
	dbPath := filepath.Join(dbDir, "s-ui.db")
	if err := Init(dbPath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeMainDB(t)
		cleanupSQLiteSidecars(dbPath)
	})

	if err := DB().Create(&model.Inbound{
		Type:    "http",
		Tag:     "no-tls",
		Addrs:   []byte("[]"),
		OutJson: []byte("{}"),
		Options: []byte("{}"),
	}).Error; err != nil {
		t.Fatal(err)
	}
	var violations int
	if err := DB().Raw("SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&violations).Error; err != nil {
		t.Fatal(err)
	}
	if violations != 0 {
		t.Fatalf("foreign key violations=%d, want 0", violations)
	}
}

func dbTestHasIndex(tx *gorm.DB, table string, indexName string) (bool, error) {
	rows, err := tx.Raw("PRAGMA index_list(" + table + ")").Rows()
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return false, err
		}
		if name == indexName {
			return true, nil
		}
	}
	return false, rows.Err()
}

type issue15PoolSetter struct {
	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime time.Duration
}

func (s *issue15PoolSetter) SetMaxOpenConns(value int) {
	s.maxOpenConns = value
}

func (s *issue15PoolSetter) SetMaxIdleConns(value int) {
	s.maxIdleConns = value
}

func (s *issue15PoolSetter) SetConnMaxLifetime(value time.Duration) {
	s.connMaxLifetime = value
}

func setIssue15DBPoolEnv(t *testing.T, maxOpenRaw *string, maxIdleRaw *string) {
	t.Helper()

	oldMaxOpen, hadMaxOpen := os.LookupEnv(dbMaxOpenConnsEnv)
	oldMaxIdle, hadMaxIdle := os.LookupEnv(dbMaxIdleConnsEnv)
	t.Cleanup(func() {
		restoreEnvValue(dbMaxOpenConnsEnv, oldMaxOpen, hadMaxOpen)
		restoreEnvValue(dbMaxIdleConnsEnv, oldMaxIdle, hadMaxIdle)
	})

	setOptionalEnv(dbMaxOpenConnsEnv, maxOpenRaw)
	setOptionalEnv(dbMaxIdleConnsEnv, maxIdleRaw)
}

func setOptionalEnv(key string, value *string) {
	if value == nil {
		_ = os.Unsetenv(key)
		return
	}
	_ = os.Setenv(key, *value)
}

func restoreEnvValue(key string, value string, hadValue bool) {
	if !hadValue {
		_ = os.Unsetenv(key)
		return
	}
	_ = os.Setenv(key, value)
}

func stringPtr(value string) *string {
	return &value
}
