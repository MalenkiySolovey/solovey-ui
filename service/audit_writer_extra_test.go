package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestAuditWriterExtraRetriesTransientWriteFailure(t *testing.T) {
	sentinel := errors.New("temporary audit storage failure")
	var attempts atomic.Int32
	wrote := make(chan []model.AuditEvent, 1)
	writer := newAuditWriter(10, 1, time.Hour, func(events []model.AuditEvent) error {
		if attempts.Add(1) == 1 {
			return sentinel
		}
		wrote <- append([]model.AuditEvent(nil), events...)
		return nil
	})
	t.Cleanup(func() {
		if err := writer.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})

	writer.Enqueue(model.AuditEvent{Event: "retry_me"})
	select {
	case events := <-wrote:
		if attempts.Load() < 2 || len(events) != 1 || events[0].Event != "retry_me" {
			t.Fatalf("unexpected retried batch: attempts=%d events=%#v", attempts.Load(), events)
		}
	case <-time.After(time.Second):
		t.Fatal("transiently failed audit batch was not retried")
	}
}

func TestAuditWriterExtraStopReportsPersistentWriteFailure(t *testing.T) {
	sentinel := errors.New("persistent audit storage failure")
	attempted := make(chan struct{}, 1)
	writer := newAuditWriter(10, 1, time.Hour, func([]model.AuditEvent) error {
		select {
		case attempted <- struct{}{}:
		default:
		}
		return sentinel
	})
	writer.Enqueue(model.AuditEvent{Event: "cannot_persist"})
	select {
	case <-attempted:
	case <-time.After(time.Second):
		t.Fatal("audit write was not attempted")
	}
	if err := writer.Stop(context.Background()); !errors.Is(err, sentinel) {
		t.Fatalf("Stop error=%v, want %v", err, sentinel)
	}
	pending := writer.drainPending()
	if len(pending) != 1 || pending[0].Event != "cannot_persist" {
		t.Fatalf("failed shutdown write was not retained for the next writer: %#v", pending)
	}
}

func TestAuditWriterExtraOverflowIncrementsDroppedTotal(t *testing.T) {
	auditDroppedTotal.Store(0)
	writer := newAuditWriter(1, 10, time.Hour, nil)

	writer.push(model.AuditEvent{Event: "first"})
	writer.push(model.AuditEvent{Event: "second"})
	writer.push(model.AuditEvent{Event: "third"})

	if got := AuditDroppedTotal(); got != 2 {
		t.Fatalf("dropped total=%d, want 2", got)
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if len(writer.queue) != 1 || writer.queue[0].Event != "third" {
		t.Fatalf("overflow should retain newest event, queue=%#v", writer.queue)
	}
}

func TestAuditWriterExtraFlushesPartialBatchOnInterval(t *testing.T) {
	wrote := make(chan []model.AuditEvent, 1)
	writer := newAuditWriter(10, 5, 20*time.Millisecond, func(events []model.AuditEvent) error {
		wrote <- append([]model.AuditEvent(nil), events...)
		return nil
	})
	defer func() {
		if err := writer.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	writer.Enqueue(model.AuditEvent{Event: "one"})
	writer.Enqueue(model.AuditEvent{Event: "two"})

	select {
	case events := <-wrote:
		if len(events) != 2 || events[0].Event != "one" || events[1].Event != "two" {
			t.Fatalf("unexpected interval batch: %#v", events)
		}
	case <-time.After(time.Second):
		t.Fatal("partial audit batch was not flushed on interval")
	}
}

func TestAuditWriterExtraStopFlushesUnsentBatch(t *testing.T) {
	wrote := make(chan []model.AuditEvent, 1)
	writer := newAuditWriter(10, 10, time.Hour, func(events []model.AuditEvent) error {
		wrote <- append([]model.AuditEvent(nil), events...)
		return nil
	})

	writer.Enqueue(model.AuditEvent{Event: "pending"})
	if err := writer.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}

	select {
	case events := <-wrote:
		if len(events) != 1 || events[0].Event != "pending" {
			t.Fatalf("unexpected stop flush batch: %#v", events)
		}
	default:
		t.Fatal("Stop did not flush pending audit batch")
	}
}
