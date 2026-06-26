package telegram

import (
	"context"
	"sync"
	"time"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"
)

const QueueCapacity = 256

type Notification struct {
	Event string
	Text  string
}

type Notifier struct {
	capacity int
	send     func(string) Result
	audit    func(string, map[string]any)
	Backoff  []time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once

	mu       sync.Mutex
	cond     *sync.Cond
	queue    []Notification
	done     chan struct{}
	doneOnce sync.Once
	started  bool
	stopped  bool
}

func NewNotifier(capacity int, send func(string) Result, audit func(string, map[string]any)) *Notifier {
	if capacity <= 0 {
		capacity = QueueCapacity
	}
	notifier := &Notifier{
		capacity: capacity,
		send:     send,
		audit:    audit,
		Backoff: []time.Duration{
			500 * time.Millisecond,
			2 * time.Second,
		},
		queue:  make([]Notification, 0, capacity),
		done:   make(chan struct{}),
		stopCh: make(chan struct{}),
	}
	notifier.cond = sync.NewCond(&notifier.mu)
	return notifier
}

func (n *Notifier) Enqueue(job Notification) {
	n.start()
	if dropped := n.push(job); dropped != nil {
		logger.Warning("telegram notifier queue overflow; dropped event: ", dropped.Event)
		n.recordAudit("notifier_overflow", map[string]any{
			"channel":      "telegram",
			"droppedEvent": dropped.Event,
			"queuedEvent":  job.Event,
		})
	}
}

func (n *Notifier) Stop(ctx context.Context) error {
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

func (n *Notifier) Started() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.started
}

func (n *Notifier) Stopped() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.stopped
}

func (n *Notifier) QueueLen() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.queue)
}

func (n *Notifier) start() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.stopped || n.started {
		return
	}
	n.started = true
	go n.run()
}

func (n *Notifier) push(job Notification) *Notification {
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

func (n *Notifier) next() (Notification, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for len(n.queue) == 0 && !n.stopped {
		n.cond.Wait()
	}
	if len(n.queue) == 0 {
		return Notification{}, false
	}
	job := n.queue[0]
	copy(n.queue, n.queue[1:])
	n.queue = n.queue[:len(n.queue)-1]
	return job, true
}

func (n *Notifier) run() {
	defer n.closeDone()
	for {
		job, ok := n.next()
		if !ok {
			return
		}
		n.deliver(job)
	}
}

func (n *Notifier) deliver(job Notification) {
	attempts := len(n.Backoff) + 1
	result := Result{ErrorClass: "unknown"}
	for attempt := 0; attempt < attempts; attempt++ {
		result = n.send(job.Text)
		if result.Success {
			return
		}
		if attempt < len(n.Backoff) {
			delay := n.Backoff[attempt]
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
		"event":      job.Event,
		"errorClass": result.ErrorClass,
		"attempts":   attempts,
	})
}

func (n *Notifier) recordAudit(event string, details map[string]any) {
	if n.audit == nil {
		return
	}
	n.audit(event, details)
}

func (n *Notifier) sleepBackoff(delay time.Duration) bool {
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

func (n *Notifier) closeStopCh() {
	n.stopOnce.Do(func() {
		close(n.stopCh)
	})
}

func (n *Notifier) closeDone() {
	n.doneOnce.Do(func() {
		close(n.done)
	})
}
