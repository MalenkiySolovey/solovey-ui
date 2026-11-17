package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/MalenkiySolovey/solovey-host/redact"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"gorm.io/gorm"
)

func TestBuildRecordRedactsAndBoundsEverySinkField(t *testing.T) {
	record, err := BuildRecord(Event{
		Actor:     "password=actor-secret " + strings.Repeat("a", AuditActorMaxBytes),
		Event:     strings.Repeat("e", AuditEventMaxBytes+1),
		Resource:  strings.Repeat("r", AuditResourceMaxBytes+1),
		Severity:  strings.Repeat("s", AuditSeverityMaxBytes+1),
		IP:        strings.Repeat("i", AuditIPMaxBytes+1),
		UserAgent: "Authorization: Bearer user-agent-secret " + strings.Repeat("界", AuditUserAgentMaxBytes),
		Details:   map[string]any{"safe": strings.Repeat("x", AuditDetailsMaxBytes+1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"actor": record.Actor, "event": record.Event, "resource": record.Resource,
		"severity": record.Severity, "ip": record.IP, "userAgent": record.UserAgent,
	} {
		if !utf8.ValidString(value) {
			t.Fatalf("%s was truncated into invalid UTF-8", name)
		}
	}
	if strings.Contains(record.Actor, "actor-secret") || strings.Contains(record.UserAgent, "user-agent-secret") {
		t.Fatalf("audit scalar fields leaked secrets: actor=%q userAgent=%q", record.Actor, record.UserAgent)
	}
	if len(record.Actor) > AuditActorMaxBytes || len(record.UserAgent) > AuditUserAgentMaxBytes {
		t.Fatalf("audit scalar field exceeded bound: actor=%d userAgent=%d", len(record.Actor), len(record.UserAgent))
	}
	if !strings.Contains(record.Actor, redact.Marker) || !strings.Contains(record.UserAgent, redact.Marker) {
		t.Fatalf("expected redaction marker in scalar fields: actor=%q userAgent=%q", record.Actor, record.UserAgent)
	}
	var details map[string]any
	if err := json.Unmarshal(record.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details["truncated"] != true {
		t.Fatalf("oversized audit details were not replaced by a bounded marker: %s", record.Details)
	}
}

func TestRecordListenFallbackUsesStableReason(t *testing.T) {
	var recorded model.AuditEvent
	service := New(func(event model.AuditEvent) {
		recorded = event
	})
	if err := service.RecordListenFallback(
		"fallback-html",
		"127.0.0.1:443",
		"127.0.0.1:8443",
		errors.New("password=listen-canary"),
		false,
	); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(recorded.Details), "listen-canary") {
		t.Fatalf("listen fallback audit leaked bind error: %s", recorded.Details)
	}
	var details map[string]any
	if err := json.Unmarshal(recorded.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details["reason"] != "listen_failed" {
		t.Fatalf("unexpected listen fallback reason: %#v", details)
	}
}

func TestWriteEventsRejectsMissingContext(t *testing.T) {
	//lint:ignore SA1012 This is the explicit missing-context rejection contract.
	if err := WriteEventsContext(nil, []model.AuditEvent{{Event: "test"}}); err == nil {
		t.Fatal("audit write accepted a missing context")
	}
}

func TestAuditTransactionSharesOwnerRollbackAndCanonicalRedaction(t *testing.T) {
	file := filepath.Join(t.TempDir(), "audit.db")
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dbsqlite.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := dbsqlite.InitializeContext(context.Background(), file, dbsqlite.Bootstrap{
		BeforeOpen: func() error { return nil },
		AfterOpen:  func(db *gorm.DB) error { return db.AutoMigrate(&model.Setting{}, &model.AuditEvent{}) },
	}); err != nil {
		t.Fatal(err)
	}
	db := dbsqlite.DB()
	rejected := errors.New("owner transaction rejected")
	writeOwner := func(tx *gorm.DB) error {
		if err := tx.Create(&model.Setting{Key: "transaction-owner", Value: "published"}).Error; err != nil {
			return err
		}
		return RecordTransaction(tx, Event{Actor: "admin", Event: "owner_commit", Resource: "panel", Details: map[string]any{"password": "audit-fixture-credential"}}, 1234)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := writeOwner(tx); err != nil {
			return err
		}
		return rejected
	}); !errors.Is(err, rejected) {
		t.Fatal("fixture rollback did not propagate owner failure")
	}
	for _, row := range []any{&model.Setting{}, &model.AuditEvent{}} {
		var count int64
		if err := db.Model(row).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatal("audit or owner write escaped shared transaction rollback")
		}
	}
	if err := db.Transaction(writeOwner); err != nil {
		t.Fatal(err)
	}
	service := New(nil)
	rows, _, err := service.ListPageFiltered(0, 20, "owner_commit", "", 0, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("committed audit history unavailable: count=%d err=%v", len(rows), err)
	}
	if rows[0].DateTime != 1234 || strings.Contains(string(rows[0].Details), "audit-fixture-credential") || !strings.Contains(string(rows[0].Details), redact.Marker) {
		t.Fatal("transaction audit changed owner timestamp or leaked credential")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := service.RecordContext(ctx, Event{Actor: "admin", Event: "cancelled_commit"}, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled synchronous audit write accepted: %v", err)
	}
	var count int64
	if err := db.Model(&model.AuditEvent{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("cancelled audit write changed history")
	}
}
