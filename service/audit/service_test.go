package audit

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/redact"
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
