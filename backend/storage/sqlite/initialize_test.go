package sqlite

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapRejectsBeforeConnectionEffects(t *testing.T) {
	for _, failure := range []string{"missing-owner", "cancelled", "preflight"} {
		t.Run(failure, func(t *testing.T) {
			if err := Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "rejected.db")
			cause := errors.New("preflight rejected")
			calls := 0
			bootstrap := Bootstrap{BeforeOpen: func() error { calls++; return cause }, AfterOpen: func(*gorm.DB) error { t.Fatal("schema owner called after rejection"); return nil }}
			ctx := context.Background()
			switch failure {
			case "missing-owner":
				bootstrap.AfterOpen = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			err := InitializeContext(ctx, path, bootstrap)
			if err == nil {
				t.Fatal("rejected bootstrap accepted")
			}
			want := 0
			if failure == "preflight" {
				want = 1
				if !errors.Is(err, cause) {
					t.Fatalf("owner cause lost: %v", err)
				}
			}
			if calls != want || DB() != nil || IsOpen() {
				t.Fatalf("rejected bootstrap acquired authority: callbacks=%d", calls)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("rejected bootstrap created a database: %v", err)
			}
		})
	}
}

func TestFailedBootstrapRetiresSameConnectionBeforeRetry(t *testing.T) {
	if err := Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := Close(); err != nil {
			t.Error(err)
		}
	})
	path := filepath.Join(t.TempDir(), "candidate.db")
	cause := errors.New("schema validation rejected")
	var rejected *gorm.DB
	err := InitializeContext(context.Background(), path, Bootstrap{BeforeOpen: func() error { return nil }, AfterOpen: func(db *gorm.DB) error {
		rejected = db
		if DB() == nil || db.Statement.ConnPool != DB().Statement.ConnPool {
			t.Fatal("schema callback received a different connection owner")
		}
		return cause
	}})
	if !errors.Is(err, cause) || DB() != nil || IsOpen() {
		t.Fatalf("failed bootstrap remained published: %v", err)
	}
	if rejected == nil {
		t.Fatal("schema callback was not reached")
	}
	if err := rejected.Exec("CREATE TABLE escaped(value TEXT)").Error; err == nil {
		t.Fatal("rejected schema owner retained a usable pool")
	}
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	accepted := DB()
	callbacks := 0
	err = InitializeContext(context.Background(), filepath.Join(t.TempDir(), "second.db"), Bootstrap{BeforeOpen: func() error { callbacks++; return nil }, AfterOpen: func(*gorm.DB) error { callbacks++; return nil }})
	if err == nil || callbacks != 0 || DB() != accepted {
		t.Fatalf("second initializer replaced live authority: callbacks=%d err=%v", callbacks, err)
	}
}
