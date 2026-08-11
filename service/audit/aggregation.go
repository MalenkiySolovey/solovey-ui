package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

const (
	DefaultDenialAggregationWindow = 5 * time.Minute
	DefaultDenialAggregationKeys   = 4096
)

type denialAggregationEntry struct {
	windowStarted time.Time
	lastSeen      time.Time
	count         uint64
}

// DenialAggregator bounds both memory and write amplification. It emits the
// first event in a window and then cumulative power-of-two checkpoints
// (2,4,8,...) so operators retain a useful magnitude signal without one audit
// row per hostile request.
type DenialAggregator struct {
	mu      sync.Mutex
	entries map[string]denialAggregationEntry
	window  time.Duration
	maxKeys int
}

func NewDenialAggregator(window time.Duration, maxKeys int) *DenialAggregator {
	if window <= 0 {
		window = DefaultDenialAggregationWindow
	}
	if maxKeys <= 0 {
		maxKeys = DefaultDenialAggregationKeys
	}
	return &DenialAggregator{
		entries: make(map[string]denialAggregationEntry),
		window:  window,
		maxKeys: maxKeys,
	}
}

func (a *DenialAggregator) Observe(event Event, now time.Time) (bool, uint64) {
	if a == nil || !HighFrequencyDenial(event.Event) {
		return true, 1
	}
	key := denialAggregationKey(event)
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.entries[key]
	if !ok || now.Sub(entry.windowStarted) >= a.window {
		if !ok && len(a.entries) >= a.maxKeys {
			a.evictOldestLocked()
		}
		entry = denialAggregationEntry{windowStarted: now}
	}
	entry.count++
	entry.lastSeen = now
	a.entries[key] = entry
	return entry.count == 1 || entry.count&(entry.count-1) == 0, entry.count
}

func (a *DenialAggregator) Reset() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.entries = make(map[string]denialAggregationEntry)
	a.mu.Unlock()
}

func (a *DenialAggregator) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	found := false
	for key, entry := range a.entries {
		if !found || entry.lastSeen.Before(oldest) {
			oldestKey = key
			oldest = entry.lastSeen
			found = true
		}
	}
	if found {
		delete(a.entries, oldestKey)
	}
}

func denialAggregationKey(event Event) string {
	reason := ""
	operation := ""
	method := ""
	if event.Details != nil {
		reason = fmt.Sprint(event.Details["reason"])
		operation = fmt.Sprint(event.Details["operation"])
		method = fmt.Sprint(event.Details["method"])
	}
	sum := sha256.Sum256([]byte(event.Event + "\x00" + event.Actor + "\x00" + event.Resource + "\x00" + event.IP + "\x00" + reason + "\x00" + operation + "\x00" + method))
	return hex.EncodeToString(sum[:])
}

func HighFrequencyDenial(event string) bool {
	switch event {
	case "login_failed", "login_blocked", "step_up_rejected", "ws_origin_rejected", "ws_rate_limited",
		"audit_rate_limited", "audit_scope_denied", "scope_denied", "request_budget_rejected", "pressure_rejected",
		"security_verification_rejected", "security_verification_rate_limited", "mfa_enrollment_rejected", "mfa_challenge_rejected",
		"request_authority_rejected":
		return true
	default:
		return false
	}
}
