package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/util/redact"
)

func flushAuditForTest(t testing.TB) {
	t.Helper()
	if err := StopAuditWriter(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAuditRecordRedactsDetails(t *testing.T) {
	auditService := &AuditService{}
	initSettingTestDB(t)

	if err := auditService.Record(AuditEvent{
		Actor:    "admin",
		Event:    "api_token_created",
		Resource: "api_token",
		Details: map[string]any{
			"token": "raw-token",
			"desc":  "automation",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := StopAuditWriter(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, err := auditService.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	var details map[string]any
	if err := json.Unmarshal(events[0].Details, &details); err != nil {
		t.Fatal(err)
	}
	if details["token"] != redact.Marker {
		t.Fatalf("token was not redacted: %#v", details["token"])
	}
	if details["desc"] != "automation" {
		t.Fatalf("non-secret detail changed: %#v", details["desc"])
	}
}

func TestAuditPruneDeletesOldEvents(t *testing.T) {
	auditService := &AuditService{}
	initSettingTestDB(t)

	old := model.AuditEvent{
		DateTime: time.Now().Add(-48 * time.Hour).Unix(),
		Actor:    "admin",
		Event:    "old",
	}
	recent := model.AuditEvent{
		DateTime: time.Now().Unix(),
		Actor:    "admin",
		Event:    "recent",
	}
	if err := dbsqlite.DB().Create(&[]model.AuditEvent{old, recent}).Error; err != nil {
		if strings.Contains(err.Error(), "no such table") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := auditService.Prune(1); err != nil {
		t.Fatal(err)
	}
	events, err := auditService.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Event != "recent" {
		t.Fatalf("unexpected events after prune: %#v", events)
	}
}

func TestAuditRecordFlushesThroughWriter(t *testing.T) {
	auditService := &AuditService{}
	initSettingTestDB(t)

	if err := auditService.Record(AuditEvent{Event: "sync_test"}); err != nil {
		t.Fatal(err)
	}
	if err := StopAuditWriter(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := dbsqlite.DB().Model(model.AuditEvent{}).Where("event = ?", "sync_test").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("sync audit record count=%d, want 1", count)
	}
}

func TestAuditRecordReturnsMarshalErrorOnly(t *testing.T) {
	auditService := &AuditService{}
	initSettingTestDB(t)
	if err := auditService.Record(AuditEvent{
		Event:   "bad_details",
		Details: map[string]any{"bad": func() {}},
	}); err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestAuditServiceRecordsListenFallback(t *testing.T) {
	initSettingTestDB(t)

	if err := (&AuditService{}).RecordListenFallback("web", "192.0.2.10:2095", ":2095", nil); err != nil {
		t.Fatal(err)
	}
	if err := StopAuditWriter(context.Background()); err != nil {
		t.Fatal(err)
	}

	var event model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "listen_fallback").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Actor != "system" || event.Resource != "network" || event.Severity != AuditSeverityWarn {
		t.Fatalf("unexpected audit event: %#v", event)
	}
	var details map[string]any
	if err := json.Unmarshal(event.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details["component"] != "web" || details["requested_addr"] != "192.0.2.10:2095" || details["fallback_addr"] != ":2095" {
		t.Fatalf("unexpected details: %#v", details)
	}
}
