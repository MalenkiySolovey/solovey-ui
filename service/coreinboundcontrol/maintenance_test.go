package coreinboundcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestAuthenticatedSnapshotsUseRestoreMaintenanceAuthority(t *testing.T) {
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	dbPath := filepath.Join(t.TempDir(), "maintenance.db")
	if err := dbsqlite.Init(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dbsqlite.Close(); err != nil {
			t.Error(err)
		}
	})
	database := dbsqlite.DB()
	inbound := model.Inbound{Id: 51, Type: "shadowsocks", Tag: "restore-authenticated", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":24826,"method":"aes-128-gcm"}`)}
	if err := database.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&model.Client{Name: "restore-principal", Enable: true, SubSecret: "synthetic-restore-subscription", Links: json.RawMessage(`[]`), Config: json.RawMessage(`{"shadowsocks":{"name":"restore-principal","password":"synthetic-restore-fixture"}}`), Inbounds: json.RawMessage(`[51]`)}).Error; err != nil {
		t.Fatal(err)
	}
	owner, err := dbsqlite.BeginMaintenance(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.End)
	ctx := owner.Context(t.Context())
	// Restore reopens a new private generation before mandatory owner rebind.
	// Merely beginning the drain does not reproduce that lifecycle phase.
	if err := dbsqlite.CloseForFileSwap(ctx); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.InitContext(ctx, dbPath); err != nil {
		t.Fatal(err)
	}
	database = dbsqlite.DB()
	service := NewWithMutations(database, nil, MutationDependencies{})
	counts, err := service.authenticationCounts(ctx, []uint{51})
	if err != nil || counts[51] != 1 {
		t.Fatalf("scoped authenticated membership: count=%d err=%v", counts[51], err)
	}
	if _, err := service.Snapshot(ctx, 51); err != nil {
		t.Fatal("scoped snapshot", err)
	}
	if snapshots, err := service.ListSnapshots(ctx, 10); err != nil || len(snapshots) != 1 {
		t.Fatalf("scoped list: count=%d err=%v", len(snapshots), err)
	}
	if _, err := service.ListSnapshots(context.Background(), 10); !errors.Is(err, dbsqlite.ErrMaintenance) {
		t.Fatalf("ordinary admission during restore: %v", err)
	}
	if err := database.WithContext(ctx).Migrator().DropTable(&model.Client{}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.authenticationCounts(ctx, []uint{51}); !errors.Is(err, ErrAuthenticationMembershipUnavailable) {
		t.Fatalf("absent schema must fail closed: %v", err)
	}
	owner.End()
	if _, err := service.ListSnapshots(ctx, 10); !errors.Is(err, dbsqlite.ErrRetired) {
		t.Fatalf("expired restore authority: %v", err)
	}
}
