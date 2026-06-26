package maintenance

import (
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/cronjob/internal/testutil"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	ipmonitor "github.com/MalenkiySolovey/solovey-ui/ipmonitor"
)

func TestCronJobTestDBIsIsolatedBetweenInitializations(t *testing.T) {
	testutil.InitDatabase(t)
	if err := dbsqlite.DB().Create(&model.ClientIP{
		ClientName: "alice",
		IP:         "198.51.100.10",
		IPHash:     "hash",
		FirstSeen:  1,
		LastSeen:   1,
	}).Error; err != nil {
		t.Fatal(err)
	}

	testutil.InitDatabase(t)
	if err := dbsqlite.DB().Create(&model.ClientIP{
		ClientName: "alice",
		IP:         "198.51.100.10",
		IPHash:     "hash",
		FirstSeen:  1,
		LastSeen:   1,
	}).Error; err != nil {
		t.Fatalf("second isolated test DB rejected duplicate unique row: %v", err)
	}
}

func TestHistoryRetentionJobPrunesAuditEventsAndClientIPs(t *testing.T) {
	testutil.InitDatabase(t)
	now := time.Now()
	oldTime := now.Add(-31 * 24 * time.Hour).Unix()
	recentTime := now.Unix()
	if err := dbsqlite.DB().Create(&[]model.AuditEvent{
		{DateTime: oldTime, Actor: "admin", Event: "old"},
		{DateTime: recentTime, Actor: "admin", Event: "recent"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&[]model.ClientIP{
		{ClientName: "alice", IP: "198.51.100.10", IPHash: "hash-old", FirstSeen: oldTime, LastSeen: oldTime},
		{ClientName: "alice", IP: "198.51.100.11", IPHash: "hash-recent", FirstSeen: recentTime, LastSeen: recentTime},
	}).Error; err != nil {
		t.Fatal(err)
	}

	NewHistoryRetentionJob().Run()

	var auditEvents []model.AuditEvent
	if err := dbsqlite.DB().Order("event asc").Find(&auditEvents).Error; err != nil {
		t.Fatal(err)
	}
	if len(auditEvents) != 1 || auditEvents[0].Event != "recent" {
		t.Fatalf("unexpected audit events after GC: %#v", auditEvents)
	}
	var clientIPs []model.ClientIP
	if err := dbsqlite.DB().Order("ip asc").Find(&clientIPs).Error; err != nil {
		t.Fatal(err)
	}
	if len(clientIPs) != 1 || clientIPs[0].IP != "198.51.100.11" {
		t.Fatalf("unexpected client IPs after GC: %#v", clientIPs)
	}
}

func TestPruneClientIPsInvalidatesIPMonitorAllowCache(t *testing.T) {
	testutil.InitDatabase(t)
	ipmonitor.InvalidateAllCache()
	oldTime := time.Now().Add(-31 * 24 * time.Hour).Unix()
	if err := dbsqlite.DB().Create(&model.Client{
		Enable:      true,
		Name:        "alice",
		LimitIP:     1,
		IPLimitMode: ipmonitor.ModeEnforce,
		Inbounds:    []byte("[]"),
		Links:       []byte("[]"),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&model.ClientIP{
		ClientName: "alice",
		IP:         "198.51.100.10",
		FirstSeen:  oldTime,
		LastSeen:   oldTime,
	}).Error; err != nil {
		t.Fatal(err)
	}

	if !ipmonitor.Allow("alice", "198.51.100.10") {
		t.Fatal("known IP should warm allow cache")
	}
	if _, err := ipmonitor.PruneOlderThan(time.Now().Add(-30 * 24 * time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if !ipmonitor.Allow("alice", "198.51.100.11") {
		t.Fatal("new IP should be allowed after pruned cached IP is removed")
	}
}
