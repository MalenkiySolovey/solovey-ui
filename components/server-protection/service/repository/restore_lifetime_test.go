package repository

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestMigrationRetainsPrivateRestoreDatabaseContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restore-context.db")
	if err := dbsqlite.Init(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	owner, err := dbsqlite.BeginMaintenance(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.End()
	ctx := owner.Context(context.Background())
	if err := dbsqlite.CloseForFileSwap(ctx); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.InitContext(ctx, path); err != nil {
		t.Fatal(err)
	}
	database := dbsqlite.DB()
	if err := database.Exec("SELECT 1").Error; !errors.Is(err, dbsqlite.ErrMaintenance) {
		t.Fatalf("ordinary candidate access = %v", err)
	}
	if err := Migrate(database.WithContext(ctx)); err != nil {
		t.Fatalf("private owner migration: %v", err)
	}
	owner.End()
	if err := database.Model(&SettingsModel{}).Count(new(int64)).Error; err != nil {
		t.Fatalf("published owner state: %v", err)
	}
}
