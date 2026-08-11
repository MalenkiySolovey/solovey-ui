package publicsurface

import (
	"strings"
	"sync"
	"sync/atomic"
)

type Observation struct {
	ResourceID     string `json:"resourceId"`
	ResourceKind   string `json:"resourceKind"`
	ComponentID    string `json:"componentId,omitempty"`
	SourceIP       string `json:"sourceIp"`
	MethodClass    string `json:"methodClass"`
	PathClass      string `json:"pathClass"`
	StatusClass    string `json:"statusClass"`
	UserAgentClass string `json:"userAgentClass"`
	BytesClass     string `json:"bytesClass"`
	DurationClass  string `json:"durationClass"`
	RateLimited    bool   `json:"rateLimited"`
	ObservedAt     int64  `json:"observedAt"`
}

const maxObservationBuffer = 4096

type Subscription struct {
	channel  chan Observation
	dropped  atomic.Uint64
	accepted atomic.Uint64
}

func (s *Subscription) Observations() <-chan Observation { return s.channel }
func (s *Subscription) Dropped() uint64                  { return s.dropped.Load() }
func (s *Subscription) Accepted() uint64                 { return s.accepted.Load() }
func (s *Subscription) Pending() int                     { return len(s.channel) }

type observationRegistry struct {
	mu      sync.Mutex
	nextID  uint64
	entries map[uint64]*Subscription
	view    atomic.Value // []*Subscription
}

func newObservationRegistry() *observationRegistry {
	r := &observationRegistry{entries: map[uint64]*Subscription{}}
	r.view.Store([]*Subscription(nil))
	return r
}

func (r *observationRegistry) Subscribe(buffer int) (*Subscription, func()) {
	if buffer < 1 {
		buffer = 1
	}
	if buffer > maxObservationBuffer {
		buffer = maxObservationBuffer
	}
	subscription := &Subscription{channel: make(chan Observation, buffer)}
	r.mu.Lock()
	id := r.nextID
	r.nextID++
	r.entries[id] = subscription
	r.storeLocked()
	r.mu.Unlock()
	var once sync.Once
	return subscription, func() {
		once.Do(func() {
			r.mu.Lock()
			delete(r.entries, id)
			r.storeLocked()
			r.mu.Unlock()
		})
	}
}

func (r *observationRegistry) Emit(value Observation) (accepted, dropped int) {
	entries, _ := r.view.Load().([]*Subscription)
	if len(entries) == 0 || !validObservation(value) {
		return 0, 0
	}
	for _, entry := range entries {
		select {
		case entry.channel <- value:
			entry.accepted.Add(1)
			accepted++
		default:
			entry.dropped.Add(1)
			dropped++
		}
	}
	return accepted, dropped
}

func (r *observationRegistry) Subscribers() int {
	entries, _ := r.view.Load().([]*Subscription)
	return len(entries)
}

func (r *observationRegistry) storeLocked() {
	entries := make([]*Subscription, 0, len(r.entries))
	for _, entry := range r.entries {
		entries = append(entries, entry)
	}
	r.view.Store(entries)
}

func validObservation(value Observation) bool {
	return value.ResourceID != "" && value.ResourceKind != "" && boundedToken(value.ResourceID, 256) && boundedToken(value.ResourceKind, 64) &&
		boundedToken(value.ComponentID, 64) && len(value.SourceIP) <= 128 &&
		boundedToken(value.MethodClass, 32) && boundedToken(value.PathClass, 128) &&
		boundedToken(value.StatusClass, 16) && boundedToken(value.UserAgentClass, 128) &&
		boundedToken(value.BytesClass, 32) && boundedToken(value.DurationClass, 32)
}

func boundedToken(value string, limit int) bool {
	if len(value) > limit || strings.ContainsAny(value, "?&#=/\\\r\n\t ") {
		return false
	}
	return true
}

var observations = newObservationRegistry()

func SubscribeObservations(buffer int) (*Subscription, func()) {
	return observations.Subscribe(buffer)
}

func EmitObservation(value Observation) (int, int) { return observations.Emit(value) }
func ObservationSubscribers() int                  { return observations.Subscribers() }
