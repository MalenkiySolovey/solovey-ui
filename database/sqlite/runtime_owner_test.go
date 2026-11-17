package sqlite

import (
	"context"
	"errors"
	storagesqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"path/filepath"
	"testing"
	"time"
)

func TestBootstrapAndBackendShareConnectionAndMaintenanceAuthority(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "shared.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := Close(); err != nil {
			t.Error(err)
		}
	})
	if DB() == nil || DB() != storagesqlite.DB() {
		t.Fatal("bootstrap and Backend use different database owners")
	}
	ctx, release, err := storagesqlite.AcquireOperation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	deadline, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	_, err = BeginMaintenance(deadline)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("legacy maintenance skipped Backend operation: %v", err)
	}
	release()
	owner, err := BeginMaintenance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.End()
	if !storagesqlite.IsMaintenanceContext(owner.Context(ctx)) {
		t.Fatal("maintenance capability belongs to a different owner")
	}
	if _, _, err := storagesqlite.AcquireOperation(context.Background()); !errors.Is(err, ErrMaintenance) {
		t.Fatalf("Backend admitted through Product maintenance: %v", err)
	}
	owner.End()
	if DB() != storagesqlite.DB() {
		t.Fatal("maintenance published a second connection owner")
	}
}
