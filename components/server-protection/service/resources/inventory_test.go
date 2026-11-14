package resources

import (
	"context"
	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	"testing"
)

type inventoryReaderFixture struct {
	snapshots, refreshes int
	observed             context.Context
}

func (r *inventoryReaderFixture) Snapshot(ctx context.Context) hostresources.ResourceSnapshot {
	r.snapshots++
	r.observed = ctx
	return hostresources.ResourceSnapshot{GeneratedAt: 10, Resources: []hostresources.ProtectableResource{{ID: "owned"}}}
}
func (r *inventoryReaderFixture) Refresh(ctx context.Context) hostresources.ResourceSnapshot {
	r.refreshes++
	r.observed = ctx
	return hostresources.ResourceSnapshot{GeneratedAt: 20, Errors: []hostresources.ResourceError{{Owner: "core", Message: "unavailable"}}}
}

func TestInventoryUsesOnlyTheSuppliedOwnerView(t *testing.T) {
	reader := &inventoryReaderFixture{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cached := Snapshot(ctx, false, reader)
	if reader.snapshots != 1 || reader.refreshes != 0 || reader.observed != ctx || cached.GeneratedAt != 10 || len(cached.Resources) != 1 {
		t.Fatalf("cached owner view not used: %#v %#v", reader, cached)
	}
	current := Snapshot(ctx, true, reader)
	if reader.snapshots != 1 || reader.refreshes != 1 || reader.observed.Err() != context.Canceled || current.GeneratedAt != 20 || len(current.Errors) != 1 || len(current.Resources) != 0 {
		t.Fatalf("refresh errors or cancellation lost: %#v %#v", reader, current)
	}
	absent := Snapshot(ctx, true, nil)
	if len(absent.Resources) != 0 || len(absent.Errors) != 1 {
		t.Fatalf("missing owner became a complete empty inventory: %#v", absent)
	}
}
