package service

import (
	"context"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/util/redact"
)

const telegramQueueCapacity = 256

type telegramNotification struct {
	event string
	text  string
}

type telegramNotifier struct {
	capacity int
	send     func(string) TelegramResult
	audit    func(string, map[string]any)
	backoff  []time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once

	mu       sync.Mutex
	cond     *sync.Cond
	queue    []telegramNotification
	done     chan struct{}
	doneOnce sync.Once
	started  bool
	stopped  bool
}

func newTelegramNotifier(capacity int, send func(string) TelegramResult, audit func(string, map[string]any)) *telegramNotifier {
	if capacity <= 0 {
		capacity = telegramQueueCapacity
	}
	notifier := &telegramNotifier{
		capacity: capacity,
		send:     send,
		audit:    audit,
		backoff: []time.Duration{
			500 * time.Millisecond,
			2 * time.Second,
		},
		queue:  make([]telegramNotification, 0, capacity),
		done:   make(chan struct{}),
		stopCh: make(chan struct{}),
	}
	notifier.cond = sync.NewCond(&notifier.mu)
	return notifier
}

func newDefaultTelegramNotifier() *telegramNotifier {
	return newTelegramNotifier(
		telegramQueueCapacity,
		func(text string) TelegramResult {
			return (&TelegramService{}).send(text)
		},
		recordTelegramNotifierAudit,
	)
}

func StopTelegramNotifier(ctx context.Context) error {
	runtime := DefaultRuntime()
	notifier := runtime.telegram()
	if notifier == nil {
		return nil
	}

	err := notifier.Stop(ctx)

	runtime.replaceTelegramNotifierIfCurrent(notifier)
	return err
}

func (n *telegramNotifier) Enqueue(job telegramNotification) {
	n.start()
	if dropped := n.push(job); dropped != nil {
		logger.Warning("telegram notifier queue overflow; dropped event: ", dropped.event)
		n.recordAudit("notifier_overflow", map[string]any{
			"channel":      "telegram",
			"droppedEvent": dropped.event,
			"queuedEvent":  job.event,
		})
	}
}

func (n *telegramNotifier) start() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.stopped || n.started {
		return
	}
	n.started = true
	go n.run()
}

func (n *telegramNotifier) push(job telegramNotification) *telegramNotification {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.stopped {
		return nil
	}
	if len(n.queue) >= n.capacity {
		dropped := n.queue[0]
		copy(n.queue, n.queue[1:])
		n.queue[len(n.queue)-1] = job
		n.cond.Signal()
		return &dropped
	}
	n.queue = append(n.queue, job)
	n.cond.Signal()
	return nil
}

func (n *telegramNotifier) next() (telegramNotification, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for len(n.queue) == 0 && !n.stopped {
		n.cond.Wait()
	}
	if len(n.queue) == 0 {
		return telegramNotification{}, false
	}
	job := n.queue[0]
	copy(n.queue, n.queue[1:])
	n.queue = n.queue[:len(n.queue)-1]
	return job, true
}

func (n *telegramNotifier) run() {
	defer n.closeDone()
	for {
		job, ok := n.next()
		if !ok {
			return
		}
		n.deliver(job)
	}
}

func (n *telegramNotifier) deliver(job telegramNotification) {
	attempts := len(n.backoff) + 1
	result := TelegramResult{ErrorClass: "unknown"}
	for attempt := 0; attempt < attempts; attempt++ {
		result = n.send(job.text)
		if result.Success {
			return
		}
		if attempt < len(n.backoff) {
			delay := n.backoff[attempt]
			if result.RetryAfter > 0 {
				delay = result.RetryAfter
			}
			if !n.sleepBackoff(delay) {
				return
			}
		}
	}
	if result.ErrorClass == "" {
		result.ErrorClass = "unknown"
	}
	logger.Warning("telegram notification failed: ", result.ErrorClass)
	n.recordAudit("notifier_failed", map[string]any{
		"channel":    "telegram",
		"event":      job.event,
		"errorClass": result.ErrorClass,
		"attempts":   attempts,
	})
}

func (n *telegramNotifier) recordAudit(event string, details map[string]any) {
	if n.audit == nil {
		return
	}
	n.audit(event, details)
}

func (n *telegramNotifier) sleepBackoff(delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-n.stopCh:
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(delay)
	select {
	case <-timer.C:
		return true
	case <-n.stopCh:
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		return false
	}
}

func (n *telegramNotifier) closeStopCh() {
	n.stopOnce.Do(func() {
		close(n.stopCh)
	})
}

func (n *telegramNotifier) closeDone() {
	n.doneOnce.Do(func() {
		close(n.done)
	})
}

func (n *telegramNotifier) Stop(ctx context.Context) error {
	n.mu.Lock()
	n.stopped = true
	n.cond.Broadcast()
	started := n.started
	done := n.done
	n.mu.Unlock()
	n.closeStopCh()
	if !started {
		n.closeDone()
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *TelegramService) NotifyTelegramEvent(event string, fields map[string]string) {
	enabled, err := s.telegramEnabled()
	if err != nil || !enabled {
		return
	}
	msg := "S-UI event: " + redact.String(event)
	for key, value := range fields {
		if value == "" {
			continue
		}
		if redact.IsSensitiveKey(key) {
			value = redact.Marker
		} else {
			value = redact.String(value)
		}
		msg += "\n" + key + ": " + value
	}
	notifier := s.runtime().telegram()
	if notifier != nil {
		notifier.Enqueue(telegramNotification{event: event, text: msg})
	}
}

func recordTelegramNotifierAudit(event string, details map[string]any) {
	if database.GetDB() == nil {
		return
	}
	if err := (&AuditService{}).Record(AuditEvent{
		Event:    event,
		Resource: "notifier",
		Severity: AuditSeverityWarn,
		Details:  details,
	}); err != nil {
		logger.Warning("telegram notifier audit failed:", err)
	}
}
