package store

import (
	"context"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"testing"
	"time"
)

func TestAuditWriterDropsOldestWhenFull(t *testing.T) {
	wrote := []model.AuditEvent{}
	auditDroppedTotal.Store(0)
	writer := NewWriter(2, 10, time.Hour, func(events []model.AuditEvent) error {
		t.Fatal("drop-oldest test should not start the writer")
		return nil
	})

	writer.push(model.AuditEvent{Event: "first"})
	writer.push(model.AuditEvent{Event: "second"})
	writer.push(model.AuditEvent{Event: "third"})

	if DroppedTotal() != 1 {
		t.Fatalf("audit dropped total=%d, want 1", DroppedTotal())
	}
	writer.mu.Lock()
	wrote = append(wrote, writer.queue...)
	writer.mu.Unlock()
	if len(wrote) != 2 || wrote[0].Event != "second" || wrote[1].Event != "third" {
		t.Fatalf("unexpected queued events after drop-oldest: %#v", wrote)
	}
}

func TestAuditWriterFlushesBatchAtBatchSize(t *testing.T) {
	wrote := make(chan []model.AuditEvent, 1)
	writer := NewWriter(10, 2, time.Hour, func(events []model.AuditEvent) error {
		copied := append([]model.AuditEvent(nil), events...)
		wrote <- copied
		return nil
	})

	writer.Enqueue(model.AuditEvent{Event: "one"})
	writer.Enqueue(model.AuditEvent{Event: "two"})
	defer func() {
		if err := writer.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	select {
	case events := <-wrote:
		if len(events) != 2 || events[0].Event != "one" || events[1].Event != "two" {
			t.Fatalf("unexpected batch: %#v", events)
		}
	case <-time.After(time.Second):
		t.Fatal("audit writer did not flush at batch size")
	}
}

func TestAuditWriterStopIsIdempotentAndRejectsLateEnqueue(t *testing.T) {
	wrote := make(chan []model.AuditEvent, 2)
	writer := NewWriter(10, 1, time.Hour, func(events []model.AuditEvent) error {
		wrote <- append([]model.AuditEvent(nil), events...)
		return nil
	})

	writer.Enqueue(model.AuditEvent{Event: "before_stop"})
	select {
	case events := <-wrote:
		if len(events) != 1 || events[0].Event != "before_stop" {
			t.Fatalf("unexpected initial write: %#v", events)
		}
	case <-time.After(time.Second):
		t.Fatal("audit writer did not flush before stop")
	}

	if err := writer.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	writer.Enqueue(model.AuditEvent{Event: "after_stop"})

	select {
	case events := <-wrote:
		t.Fatalf("late enqueue flushed after stop: %#v", events)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestAuditWriterStopBeforeStartPreventsStart(t *testing.T) {
	writer := NewWriter(10, 1, time.Hour, func(events []model.AuditEvent) error {
		t.Fatalf("stopped writer flushed events: %#v", events)
		return nil
	})
	if err := writer.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	writer.Enqueue(model.AuditEvent{Event: "after_stop"})
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if !writer.stopped || writer.started || len(writer.queue) != 0 {
		t.Fatalf("unexpected writer state after stop-before-start: started=%v stopped=%v queue=%d", writer.started, writer.stopped, len(writer.queue))
	}
}
