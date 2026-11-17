package accounting

import (
	"context"
	"fmt"
	entitytypes "github.com/MalenkiySolovey/solovey-ui/backend/entityidentity"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"gorm.io/gorm"
	"path/filepath"
	"testing"
)

func initSettingTestDB(tb testing.TB) {
	tb.Helper()
	if err := dbsqlite.Close(); err != nil {
		tb.Fatal(err)
	}
	if err := dbsqlite.InitializeContext(context.Background(), filepath.Join(tb.TempDir(), "panel.db"), dbsqlite.Bootstrap{
		BeforeOpen: func() error { return nil },
		AfterOpen: func(db *gorm.DB) error {
			return db.AutoMigrate(&model.Client{}, &model.Stats{}, &model.ClientIP{}, &model.Setting{})
		},
	}); err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := dbsqlite.Close(); err != nil {
			tb.Error(err)
		}
	})
}

func TestUpdateClientTrafficDeltasBatchesTwoHundredClients(t *testing.T) {
	initSettingTestDB(t)

	clients := make([]model.Client, 0, 200)
	deltas := make(map[entitytypes.VPNUserID]clientTrafficDelta, 200)
	expectedUp := make(map[string]int64, 200)
	expectedDown := make(map[string]int64, 200)
	for i := 0; i < 200; i++ {
		name := fmt.Sprintf("client-%03d", i)
		initialUp := int64(i)
		initialDown := int64(i * 2)
		upDelta := int64(i + 10)
		downDelta := int64(i + 20)
		clients = append(clients, model.Client{
			Id:   uint(i + 1),
			Name: name,
			Up:   initialUp,
			Down: initialDown,
		})
		deltas[entitytypes.VPNUserID(i+1)] = clientTrafficDelta{
			up:   upDelta,
			down: downDelta,
		}
		expectedUp[name] = initialUp + upDelta
		expectedDown[name] = initialDown + downDelta
	}
	if err := dbsqlite.DB().Create(&clients).Error; err != nil {
		t.Fatal(err)
	}

	if err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		return updateClientTrafficDeltas(tx, deltas)
	}); err != nil {
		t.Fatal(err)
	}

	var stored []model.Client
	if err := dbsqlite.DB().Order("name asc").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 200 {
		t.Fatalf("expected 200 clients, got %d", len(stored))
	}
	for _, client := range stored {
		if client.Up != expectedUp[client.Name] || client.Down != expectedDown[client.Name] {
			t.Fatalf("unexpected traffic for %s: up=%d down=%d", client.Name, client.Up, client.Down)
		}
	}
}

func TestStatsServiceDownsampleStatsBucketsExtra(t *testing.T) {
	input := []model.Stats{
		{DateTime: 100, Resource: "user", Tag: "alice", Direction: false, Traffic: 10},
		{DateTime: 101, Resource: "user", Tag: "alice", Direction: false, Traffic: 30},
		{DateTime: 102, Resource: "user", Tag: "alice", Direction: true, Traffic: 40},
		{DateTime: 110, Resource: "user", Tag: "alice", Direction: false, Traffic: 90},
		{DateTime: 111, Resource: "user", Tag: "alice", Direction: true, Traffic: 100},
		{DateTime: 112, Resource: "user", Tag: "alice", Direction: true, Traffic: 140},
	}

	got := downsampleStats(input, 4)
	if len(got) != 4 {
		t.Fatalf("expected 4 downsampled rows, got %d: %#v", len(got), got)
	}
	if got[0].Direction || got[0].Traffic != 20 {
		t.Fatalf("unexpected first bucket down row: %#v", got[0])
	}
	if !got[1].Direction || got[1].Traffic != 40 {
		t.Fatalf("unexpected first bucket up row: %#v", got[1])
	}
	if got[2].Direction || got[2].Traffic != 90 {
		t.Fatalf("unexpected second bucket down row: %#v", got[2])
	}
	if !got[3].Direction || got[3].Traffic != 120 {
		t.Fatalf("unexpected second bucket up row: %#v", got[3])
	}
	if got[0].Resource != "user" || got[0].Tag != "alice" {
		t.Fatalf("resource/tag not preserved: %#v", got[0])
	}
	if got[0].DateTime > got[2].DateTime {
		t.Fatalf("bucket order regressed: %#v", got)
	}
}
