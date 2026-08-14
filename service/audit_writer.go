package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

const (
	auditQueueCapacity = 4096
	auditBatchSize     = 64
	auditFlushInterval = 200 * time.Millisecond
	auditRetryInterval = 100 * time.Millisecond

	// Coverage-gap signal: once this many audit events have been dropped since
	// the last marker (and the window has elapsed), emit one synchronous warn
	// event so a sustained drop (overload / tamper attempt) is itself audited.
	auditDropMarkerThreshold = 100
	auditDropMarkerWindow    = 60 * time.Second
)

var (
	auditDroppedTotal   atomic.Uint64
	auditDropMarkerMu   sync.Mutex
	auditDropMarkerAt   time.Time
	auditDropMarkerBase uint64
)

type auditWriter struct {
	capacity      int
	batchSize     int
	flushInterval time.Duration
	write         func([]model.AuditEvent) error

	mu      sync.Mutex
	queue   []model.AuditEvent
	notify  chan struct{}
	stopCh  chan struct{}
	done    chan struct{}
	started bool
	stopped bool
	stopErr error
}

func newAuditWriter(capacity int, batchSize int, flushInterval time.Duration, write func([]model.AuditEvent) error) *auditWriter {
	if capacity <= 0 {
		capacity = auditQueueCapacity
	}
	if batchSize <= 0 {
		batchSize = auditBatchSize
	}
	if flushInterval <= 0 {
		flushInterval = auditFlushInterval
	}
	return &auditWriter{
		capacity:      capacity,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		write:         write,
		queue:         make([]model.AuditEvent, 0, capacity),
		notify:        make(chan struct{}, 1),
		stopCh:        make(chan struct{}),
		done:          make(chan struct{}),
	}
}

func StopAuditWriter(ctx context.Context) error {
	return DefaultRuntime().StopAuditWriter(ctx)
}

func (r *Runtime) StopAuditWriter(ctx context.Context) error {
	r = runtimeOrDefault(r)
	writer := r.audit()
	if writer == nil {
		return nil
	}

	err := writer.Stop(ctx)
	r.replaceAuditWriterIfCurrent(writer, writer.drainPending())
	return err
}

func (w *auditWriter) Enqueue(event model.AuditEvent) {
	if !w.Start() {
		return
	}
	w.push(event)
	w.maybeEmitDropMarker()
}

// maybeEmitDropMarker enqueues a single warn-level "audit_events_dropped" event
// when drops have crossed the threshold within a window. It is called only from
// Enqueue (never from push), so it cannot recurse; the marker is warn-priority
// so the overflow victim logic will not silently drop it (T1070 detection).
func (w *auditWriter) maybeEmitDropMarker() {
	total := auditDroppedTotal.Load()
	auditDropMarkerMu.Lock()
	since := total - auditDropMarkerBase
	now := time.Now()
	if since < auditDropMarkerThreshold || now.Sub(auditDropMarkerAt) < auditDropMarkerWindow {
		auditDropMarkerMu.Unlock()
		return
	}
	auditDropMarkerBase = total
	auditDropMarkerAt = now
	auditDropMarkerMu.Unlock()

	marker, err := buildAuditRecord(AuditEvent{
		Actor:    "system",
		Event:    "audit_events_dropped",
		Resource: "audit",
		Severity: AuditSeverityWarn,
		Details:  map[string]any{"droppedTotal": total, "sinceLast": since},
	})
	if err != nil {
		return
	}
	w.push(marker)
}

func (w *auditWriter) push(event model.AuditEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return
	}
	if len(w.queue) >= w.capacity {
		victim := auditOverflowVictimIndex(w.queue, event)
		if victim >= 0 {
			copy(w.queue[victim:], w.queue[victim+1:])
			w.queue[len(w.queue)-1] = event
		}
		auditDroppedTotal.Add(1)
		w.signalLocked()
		return
	}
	w.queue = append(w.queue, event)
	w.signalLocked()
}

func auditOverflowVictimIndex(queue []model.AuditEvent, event model.AuditEvent) int {
	for i, queued := range queue {
		if auditLowPriority(queued) {
			return i
		}
	}
	if auditLowPriority(event) {
		return -1
	}
	return 0
}

func auditLowPriority(event model.AuditEvent) bool {
	return event.Severity == "" || event.Severity == AuditSeverityInfo
}

func (w *auditWriter) Start() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return false
	}
	if w.started {
		return true
	}
	w.started = true
	go w.run()
	return true
}

func (w *auditWriter) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.started {
		w.stopped = true
		w.mu.Unlock()
		return nil
	}
	if !w.stopped {
		w.stopped = true
		close(w.stopCh)
	}
	done := w.done
	w.mu.Unlock()

	select {
	case <-done:
		w.mu.Lock()
		err := w.stopErr
		w.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *auditWriter) run() {
	defer close(w.done)
	for {
		batch := w.popBatch(w.batchSize)
		if len(batch) == 0 {
			select {
			case <-w.notify:
				continue
			case <-w.stopCh:
				w.recordStopError(w.flushRemaining())
				return
			}
		}

		timer := time.NewTimer(w.flushInterval)
		flush := false
		for len(batch) < w.batchSize && !flush {
			more := w.popBatch(w.batchSize - len(batch))
			if len(more) > 0 {
				batch = append(batch, more...)
				continue
			}
			select {
			case <-w.notify:
			case <-timer.C:
				flush = true
			case <-w.stopCh:
				stopTimer(timer)
				if err := w.writeBatch(batch); err != nil {
					w.restoreBatch(batch)
					w.recordStopError(err)
					return
				}
				w.recordStopError(w.flushRemaining())
				return
			}
		}
		stopTimer(timer)
		if !w.writeBatchUntilStop(batch) {
			w.recordStopError(w.flushRemaining())
			return
		}
	}
}

func (w *auditWriter) popBatch(limit int) []model.AuditEvent {
	w.mu.Lock()
	defer w.mu.Unlock()
	if limit <= 0 || len(w.queue) == 0 {
		return nil
	}
	if limit > len(w.queue) {
		limit = len(w.queue)
	}
	batch := make([]model.AuditEvent, limit)
	copy(batch, w.queue[:limit])
	copy(w.queue, w.queue[limit:])
	clear(w.queue[len(w.queue)-limit:])
	w.queue = w.queue[:len(w.queue)-limit]
	return batch
}

func (w *auditWriter) flushRemaining() error {
	var result error
	for {
		batch := w.popBatch(w.batchSize)
		if len(batch) == 0 {
			return result
		}
		if err := w.writeBatch(batch); err != nil {
			w.restoreBatch(batch)
			return errors.Join(result, err)
		}
	}
}

func (w *auditWriter) writeBatch(batch []model.AuditEvent) error {
	if len(batch) == 0 || w.write == nil {
		return nil
	}
	if err := w.write(batch); err != nil {
		logger.Warning("audit writer flush failed:", err)
		return err
	}
	return nil
}

func (w *auditWriter) writeBatchUntilStop(batch []model.AuditEvent) bool {
	for {
		err := w.writeBatch(batch)
		if err == nil {
			return true
		}
		timer := time.NewTimer(auditRetryInterval)
		select {
		case <-timer.C:
			continue
		case <-w.stopCh:
			stopTimer(timer)
			w.restoreBatch(batch)
			w.recordStopError(err)
			return false
		}
	}
}

func (w *auditWriter) restoreBatch(batch []model.AuditEvent) {
	if len(batch) == 0 {
		return
	}
	w.mu.Lock()
	combined := make([]model.AuditEvent, 0, min(w.capacity, len(batch)+len(w.queue)))
	combined = append(combined, batch...)
	combined = append(combined, w.queue...)
	if len(combined) > w.capacity {
		auditDroppedTotal.Add(uint64(len(combined) - w.capacity))
		combined = combined[:w.capacity]
	}
	w.queue = combined
	w.mu.Unlock()
}

func (w *auditWriter) drainPending() []model.AuditEvent {
	w.mu.Lock()
	defer w.mu.Unlock()
	pending := append([]model.AuditEvent(nil), w.queue...)
	clear(w.queue)
	w.queue = nil
	return pending
}

func (w *auditWriter) recordStopError(err error) {
	if err == nil {
		return
	}
	w.mu.Lock()
	w.stopErr = errors.Join(w.stopErr, err)
	w.mu.Unlock()
}

func (w *auditWriter) signalLocked() {
	select {
	case w.notify <- struct{}{}:
	default:
	}
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
