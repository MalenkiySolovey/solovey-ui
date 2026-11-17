package sqlite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDBPoolConfigFromEnv(t *testing.T) {
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
			setDBPoolEnv(t, tc.maxOpenRaw, tc.maxIdleRaw)

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

func TestApplyDBPoolConfig(t *testing.T) {
	pool := &recordingDBPoolSetter{}
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

func TestOpenDBUsesConfiguredMaxOpenConns(t *testing.T) {
	setDBPoolEnv(t, stringPtr("2"), stringPtr("1"))

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

type recordingDBPoolSetter struct {
	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime time.Duration
}

func (s *recordingDBPoolSetter) SetMaxOpenConns(value int) {
	s.maxOpenConns = value
}

func (s *recordingDBPoolSetter) SetMaxIdleConns(value int) {
	s.maxIdleConns = value
}

func (s *recordingDBPoolSetter) SetConnMaxLifetime(value time.Duration) {
	s.connMaxLifetime = value
}

func setDBPoolEnv(t *testing.T, maxOpenRaw *string, maxIdleRaw *string) {
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
