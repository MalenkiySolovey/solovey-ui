package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
)

func TestWriterOverflowProducesOnePriorityCoverageMarker(t *testing.T) {
	previousTotal := auditDroppedTotal.Swap(0)
	auditDropMarkerMu.Lock()
	previousAt, previousBase := auditDropMarkerAt, auditDropMarkerBase
	auditDropMarkerAt, auditDropMarkerBase = time.Time{}, 0
	auditDropMarkerMu.Unlock()
	t.Cleanup(func() {
		auditDroppedTotal.Store(previousTotal)
		auditDropMarkerMu.Lock()
		auditDropMarkerAt, auditDropMarkerBase = previousAt, previousBase
		auditDropMarkerMu.Unlock()
	})
	writer := NewWriter(2, 10, time.Hour, nil)
	writer.PushPending(model.AuditEvent{Event: "authorization_denied", Severity: AuditSeverityWarn})
	writer.PushPending(model.AuditEvent{Event: "ordinary", Severity: AuditSeverityInfo})
	for i := 0; i < auditDropMarkerThreshold; i++ {
		writer.PushPending(model.AuditEvent{Event: "ordinary", Severity: AuditSeverityInfo})
	}
	writer.maybeEmitDropMarker()
	writer.maybeEmitDropMarker()
	pending := writer.DrainPending()
	if len(pending) != 2 || pending[0].Event != "authorization_denied" || pending[1].Event != "audit_events_dropped" || pending[1].Severity != AuditSeverityWarn {
		t.Fatalf("overflow lost priority security event or repeated coverage marker: %#v", pending)
	}
	var details map[string]any
	if err := json.Unmarshal(pending[1].Details, &details); err != nil {
		t.Fatal(err)
	}
	if details["droppedTotal"] != float64(auditDropMarkerThreshold) || details["sinceLast"] != float64(auditDropMarkerThreshold) {
		t.Fatalf("coverage marker reports wrong drop counts: %#v", details)
	}
	if writer.started {
		t.Fatal("successor queue started a worker before Product admission")
	}
}
