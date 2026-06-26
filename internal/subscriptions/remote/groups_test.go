package remote

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newRemoteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.RemoteOutboundSubscription{},
		&model.RemoteOutboundGroup{},
		&model.RemoteOutboundGroupConnection{},
		&model.RemoteOutboundConnection{},
	); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	return db
}

func TestEnsureDefaultGroupIsIdempotent(t *testing.T) {
	db := newRemoteDB(t)
	first, err := EnsureDefaultGroup(db, 7, 100)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EnsureDefaultGroup(db, 7, 200)
	if err != nil {
		t.Fatal(err)
	}
	if first.Id != second.Id {
		t.Fatalf("default group should be reused: first=%d second=%d", first.Id, second.Id)
	}
	if second.Name != DefaultGroupName {
		t.Fatalf("default group name = %q", second.Name)
	}
}

func TestHydrateConnectionGroupIDs(t *testing.T) {
	db := newRemoteDB(t)
	sub := model.RemoteOutboundSubscription{Name: "sub"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	groupA := model.RemoteOutboundGroup{SubscriptionId: sub.Id, Name: "A"}
	groupB := model.RemoteOutboundGroup{SubscriptionId: sub.Id, Name: "B"}
	if err := db.Create(&[]*model.RemoteOutboundGroup{&groupA, &groupB}).Error; err != nil {
		t.Fatal(err)
	}
	connection := model.RemoteOutboundConnection{SubscriptionId: sub.Id, Name: "node", SourceKey: "node", OutboundTag: "ros-node"}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	if err := AddGroupConnection(db, groupB.Id, connection.Id, 1); err != nil {
		t.Fatal(err)
	}
	if err := AddGroupConnection(db, groupA.Id, connection.Id, 1); err != nil {
		t.Fatal(err)
	}

	subscriptions := []model.RemoteOutboundSubscription{{
		Id:          sub.Id,
		Connections: []model.RemoteOutboundConnection{connection},
	}}
	if err := HydrateConnectionGroupIDs(db, subscriptions); err != nil {
		t.Fatal(err)
	}
	got := subscriptions[0].Connections[0].GroupIds
	if len(got) != 2 || got[0] != groupA.Id || got[1] != groupB.Id {
		t.Fatalf("group ids = %#v, want [%d %d]", got, groupA.Id, groupB.Id)
	}
}

func TestReconcileGroupStatesDisablesIncompleteGroup(t *testing.T) {
	db := newRemoteDB(t)
	sub := model.RemoteOutboundSubscription{Name: "sub"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	group := model.RemoteOutboundGroup{SubscriptionId: sub.Id, Name: "sync", OutboundEnabled: true}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	connection := model.RemoteOutboundConnection{
		SubscriptionId: sub.Id,
		Name:           "node",
		SourceKey:      "node",
		OutboundTag:    "ros-node",
		Enabled:        true,
		Missing:        false,
		Synced:         false,
	}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	if err := AddGroupConnection(db, group.Id, connection.Id, 1); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileGroupStates(db); err != nil {
		t.Fatal(err)
	}
	var stored model.RemoteOutboundGroup
	if err := db.First(&stored, group.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.OutboundEnabled {
		t.Fatal("group with unsynced active connection should be disabled")
	}
}

func TestFilterUsableConnectionsAndAllSynced(t *testing.T) {
	connections := []model.RemoteOutboundConnection{
		{Enabled: true, Missing: false, Synced: true},
		{Enabled: false, Missing: false, Synced: true},
		{Enabled: true, Missing: true, Synced: true},
	}
	active := FilterUsableConnections(connections)
	if len(active) != 1 {
		t.Fatalf("active = %#v, want one", active)
	}
	if !ConnectionsAllSynced(active) {
		t.Fatal("single synced active connection should be all-synced")
	}
	if ConnectionsAllSynced(nil) {
		t.Fatal("empty list must not be all-synced")
	}
}
