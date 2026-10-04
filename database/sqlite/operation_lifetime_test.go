package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func lifetimeDatabase(t *testing.T) (*sql.DB, string) {
	t.Helper()
	if err := Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "generation.db")
	if err := open(path); err != nil {
		if strings.Contains(err.Error(), "requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := Close(); err != nil {
			t.Error(err)
		}
	})
	if err := DB().Exec("CREATE TABLE lifetime_rows(value TEXT); INSERT INTO lifetime_rows VALUES('original'),('second')").Error; err != nil {
		t.Fatal(err)
	}
	pool, err := DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	return pool, path
}

func waitLifetimeState(t *testing.T, predicate func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		dbMu.RLock()
		ready, changed := predicate(), lifetimeChanged
		dbMu.RUnlock()
		if ready {
			return
		}
		select {
		case <-changed:
		case <-ctx.Done():
			t.Fatal("lifetime barrier was not reached")
		}
	}
}

func holdSQLResource(t *testing.T, pool *sql.DB, kind string) func() error {
	t.Helper()
	var release func() error
	switch kind {
	case "rows":
		rows, err := pool.Query("SELECT value FROM lifetime_rows")
		if err != nil {
			t.Fatal(err)
		}
		if !rows.Next() {
			t.Fatal("reader not established")
		}
		release = rows.Close
	case "row":
		row := pool.QueryRow("SELECT value FROM lifetime_rows LIMIT 1")
		release = func() error { var value string; return row.Scan(&value) }
	case "transaction":
		tx, err := pool.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec("UPDATE lifetime_rows SET value = 'committed' WHERE value = 'original'"); err != nil {
			t.Fatal(err)
		}
		release = tx.Commit
	case "borrowed_connection":
		conn, err := pool.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var value string
		if err := conn.QueryRowContext(context.Background(), "SELECT value FROM lifetime_rows LIMIT 1").Scan(&value); err != nil {
			t.Fatal(err)
		}
		release = conn.Close
	case "raw_callback":
		conn, err := pool.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		entered, leave, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		go func() { done <- conn.Raw(func(any) error { close(entered); <-leave; return nil }) }()
		<-entered
		release = func() error { close(leave); return errors.Join(<-done, conn.Close()) }
	case "prepared_rows":
		stmt, err := pool.Prepare("SELECT value FROM lifetime_rows")
		if err != nil {
			t.Fatal(err)
		}
		rows, err := stmt.Query()
		if err != nil {
			t.Fatal(err)
		}
		if !rows.Next() {
			t.Fatal("statement reader not established")
		}
		release = func() error { return errors.Join(rows.Close(), stmt.Close()) }
	default:
		t.Fatal("unknown resource")
	}
	var once sync.Once
	var err error
	finish := func() error { once.Do(func() { err = release() }); return err }
	t.Cleanup(func() { _ = finish() })
	return finish
}

func TestFileSwapDrainsSupportedSQLResourceLifetimes(t *testing.T) {
	for _, kind := range []string{"rows", "row", "transaction", "borrowed_connection", "raw_callback", "prepared_rows"} {
		t.Run(kind, func(t *testing.T) {
			pool, _ := lifetimeDatabase(t)
			old := DB()
			stmt, err := pool.Prepare("SELECT value FROM lifetime_rows LIMIT 1")
			if err != nil {
				t.Fatal(err)
			}
			defer stmt.Close()
			release := holdSQLResource(t, pool, kind)
			done := make(chan error, 1)
			go func() { done <- CloseForFileSwap(context.Background()) }()
			waitLifetimeState(t, func() bool { return activeGeneration != nil && activeGeneration.phase == generationDraining })
			select {
			case err := <-done:
				t.Fatalf("file swap closed a live %s: %v", kind, err)
			default:
			}
			if DB() == nil {
				t.Fatal("active drain published nil")
			}
			var count int64
			if err := old.Raw("SELECT COUNT(*) FROM lifetime_rows").Scan(&count).Error; !errors.Is(err, ErrMaintenance) {
				t.Fatalf("late GORM query: %v", err)
			}
			if _, err := pool.Conn(context.Background()); !errors.Is(err, ErrMaintenance) {
				t.Fatalf("late raw SQL borrow: %v", err)
			}
			if err := release(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("released resources did not drain")
			}
			if DB() != nil || currentDatabasePath() != "" {
				t.Fatal("standalone close did not report final nil state")
			}
			if err := old.Raw("SELECT 1").Scan(&count).Error; err == nil {
				t.Fatal("retired GORM handle accepted work")
			}
			if err := stmt.QueryRow().Scan(&count); err == nil {
				t.Fatal("retired statement accepted work")
			}
		})
	}
}

func TestFileSwapCancellationKeepsOriginalGenerationHealthy(t *testing.T) {
	pool, path := lifetimeDatabase(t)
	old := DB()
	conn, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if err := CloseForFileSwap(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bounded drain: %v", err)
	}
	if DB() != old || currentDatabasePath() != path {
		t.Fatal("failed drain changed live identity")
	}
	var value string
	if err := conn.QueryRowContext(context.Background(), "SELECT value FROM lifetime_rows LIMIT 1").Scan(&value); err != nil || value != "original" {
		t.Fatalf("original connection recovery: %v", err)
	}
	ctx, release, err := AcquireOperation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := DB().WithContext(ctx).Exec("INSERT INTO lifetime_rows VALUES('healthy')").Error; err != nil {
		t.Fatal(err)
	}
	if pool.Stats().MaxOpenConnections != resolvedDBPoolConfig().maxOpenConns {
		t.Fatal("normal pool settings were not restored")
	}
}

func TestMaintenancePrivateCandidateGapExpiryAndRequestReadmission(t *testing.T) {
	t.Setenv(dbMaxOpenConnsEnv, "8")
	t.Setenv(dbMaxIdleConnsEnv, "4")
	_, path := lifetimeDatabase(t)
	requestCtx, release, err := AcquireOperation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	nested, finishNested, err := AcquireOperation(requestCtx)
	if err != nil {
		t.Fatal(err)
	}
	finishNested()
	if operationFrom(nested) != operationFrom(requestCtx) {
		t.Fatal("nested admission changed owner")
	}
	owner, err := BeginMaintenance(requestCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.End()
	ownerCtx := owner.Context(context.Background())
	if _, _, err := AcquireOperation(context.Background()); !errors.Is(err, ErrMaintenance) {
		t.Fatalf("late semantic admission: %v", err)
	}
	if err := CloseForFileSwap(ownerCtx); err != nil {
		t.Fatal(err)
	}
	gap := DB()
	if gap == nil {
		t.Fatal("maintenance gap published nil")
	}
	var count int64
	if err := gap.Raw("SELECT 1").Scan(&count).Error; !errors.Is(err, ErrMaintenance) {
		t.Fatalf("gap query: %v", err)
	}
	gapPool, err := gap.DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gapPool.Conn(context.Background()); !errors.Is(err, ErrMaintenance) {
		t.Fatalf("gap SQL authority: %v", err)
	}
	if err := Init(path); !errors.Is(err, ErrMaintenance) {
		t.Fatalf("ordinary initializer used private gap: %v", err)
	}
	if err := InitContext(ownerCtx, path); err != nil {
		t.Fatal(err)
	}
	candidate := DB()
	if maintenanceFrom(candidate.Statement.Context) != nil {
		t.Fatal("public handle leaked private context")
	}
	if err := candidate.Raw("SELECT 1").Scan(&count).Error; !errors.Is(err, ErrMaintenance) {
		t.Fatalf("candidate admitted ordinary query: %v", err)
	}
	if err := candidate.WithContext(ownerCtx).Raw("SELECT COUNT(*) FROM lifetime_rows").Scan(&count).Error; err != nil || count != 2 {
		t.Fatalf("private candidate query: %v", err)
	}
	owner.End()
	if _, err := BeginMaintenance(ownerCtx); !errors.Is(err, ErrRetired) {
		t.Fatalf("expired capability began new maintenance: %v", err)
	}
	if err := CloseContext(ownerCtx); !errors.Is(err, ErrRetired) || DB() != candidate {
		t.Fatalf("expired capability closed the published generation: %v", err)
	}
	if err := InitContext(ownerCtx, path); !errors.Is(err, ErrRetired) {
		t.Fatalf("expired capability initialized a generation: %v", err)
	}
	if err := candidate.WithContext(ownerCtx).Raw("SELECT 1").Scan(&count).Error; !errors.Is(err, ErrRetired) {
		t.Fatalf("expired capability retained authority: %v", err)
	}
	if err := candidate.WithContext(requestCtx).Raw("SELECT COUNT(*) FROM lifetime_rows").Scan(&count).Error; err != nil || count != 2 {
		t.Fatalf("request was not readmitted: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := BeginMaintenance(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("response lifetime did not protect new generation: %v", err)
	}
	release()
	if _, _, err := AcquireOperation(requestCtx); !errors.Is(err, ErrRetired) {
		t.Fatalf("released request regained authority: %v", err)
	}
	owner, err = BeginMaintenance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	owner.End()
}

func TestTerminalCloseCannotLoseAnUndrainedRetiredGeneration(t *testing.T) {
	pool, path := lifetimeDatabase(t)
	conn, err := pool.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	if err := CloseContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("terminal close claimed borrowed connection ended: %v", err)
	}
	cancel()
	if DB() != nil {
		t.Fatal("terminal close did not detach public handle")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	if err := InitContext(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("initializer forgot retired connection: %v", err)
	}
	cancel()
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
}

type failedCloseConn struct {
	driver.Conn
	err error
}

func (c failedCloseConn) Close() error { return c.err }
func TestDriverCloseFailureIsNeverCountedAsDrained(t *testing.T) {
	cause := errors.New("driver close failed")
	generation := &generation{connections: 1}
	conn := &generationConn{conn: failedCloseConn{err: cause}, generation: generation}
	if err := conn.Close(); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	if err := waitGeneration(context.Background(), generation, true); !errors.Is(err, cause) {
		t.Fatal("unproven driver close was accepted")
	}
	dbMu.RLock()
	count := generation.connections
	dbMu.RUnlock()
	if count != 1 {
		t.Fatal("failed connection was removed from lifetime authority")
	}
}
