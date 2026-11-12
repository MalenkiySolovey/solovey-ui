//go:build !minimal

package fallbackhtml

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/durableowner"
)

func durableBindingForTest(t *testing.T) *durableowner.Binding {
	t.Helper()
	component, ok := registry.ComponentByID(id)
	if !ok || component.Durable == nil {
		t.Fatal("typed Fallback durable owner is not registered")
	}
	return component.Durable
}

var dropSequence atomic.Uint64

// Unit-level authority fixture. End-to-end tests use DataLifecycle.Execute,
// including actual pre-drop backup, revision transitions and postconditions.
func authorizedDropForTest(t *testing.T, ctx context.Context) error {
	t.Helper()
	db := dbsqlite.DB()
	row := model.DataLifecycleOperation{OperationID: fmt.Sprintf("drop-fixture:%d", dropSequence.Add(1)),
		IdempotencyKey: fmt.Sprintf("drop-fixture-key:%d", dropSequence.Load()), OwnerID: id, Kind: "DROP_DATA", State: "DROPPING", Revision: 1,
		BackupRef: strings.Repeat("a", 64)}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	bound, err := durableowner.ContextForDrop(ctx, db, id, row.OperationID, row.Revision)
	if err != nil {
		return err
	}
	return durableBindingForTest(t).DropData(bound)
}
