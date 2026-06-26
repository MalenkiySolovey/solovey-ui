package ratelimit

import (
	"sync"
	"time"
)

type ThresholdDecision struct {
	Count     int
	Triggered bool
}

type thresholdEntry struct {
	windowStart time.Time
	updatedAt   time.Time
	count       int
	triggered   bool
}

type ThresholdWindow[K comparable] struct {
	mu        sync.Mutex
	entries   map[K]thresholdEntry
	window    time.Duration
	threshold int
	maxKeys   int
	gcEvery   time.Duration
	lastGC    time.Time
	janitor   *janitor
}

func NewThresholdWindow[K comparable](window time.Duration, threshold, maxKeys int, gcEvery time.Duration) *ThresholdWindow[K] {
	if window <= 0 || threshold <= 0 || maxKeys <= 0 {
		panic("ratelimit: invalid threshold window configuration")
	}
	l := &ThresholdWindow[K]{entries: make(map[K]thresholdEntry), window: window, threshold: threshold, maxKeys: maxKeys, gcEvery: gcEvery}
	l.janitor = startJanitor(gcEvery, l.PruneAt)
	return l
}

func (l *ThresholdWindow[K]) Add(key K) ThresholdDecision {
	return l.AddAt(key, time.Now())
}

func (l *ThresholdWindow[K]) AddAt(key K, now time.Time) ThresholdDecision {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maybePruneLocked(now)
	entry, exists := l.entries[key]
	if !exists {
		l.makeRoomLocked()
		entry = thresholdEntry{windowStart: now}
	} else if now.Sub(entry.windowStart) >= l.window {
		entry = thresholdEntry{windowStart: now}
	}
	entry.count++
	entry.updatedAt = now
	trigger := entry.count >= l.threshold && !entry.triggered
	if trigger {
		entry.triggered = true
	}
	l.entries[key] = entry
	return ThresholdDecision{Count: entry.count, Triggered: trigger}
}

func (l *ThresholdWindow[K]) ResetAll() {
	l.mu.Lock()
	l.entries = make(map[K]thresholdEntry)
	l.mu.Unlock()
}

func (l *ThresholdWindow[K]) PruneAt(now time.Time) {
	l.mu.Lock()
	l.pruneLocked(now)
	l.mu.Unlock()
}

func (l *ThresholdWindow[K]) Close() {
	l.janitor.close()
}

func (l *ThresholdWindow[K]) pruneLocked(now time.Time) {
	for key, entry := range l.entries {
		if now.Sub(entry.windowStart) >= l.window {
			delete(l.entries, key)
		}
	}
	l.lastGC = now
}

func (l *ThresholdWindow[K]) maybePruneLocked(now time.Time) {
	if len(l.entries) < l.maxKeys && (l.gcEvery <= 0 || (!l.lastGC.IsZero() && now.Sub(l.lastGC) < l.gcEvery)) {
		return
	}
	l.pruneLocked(now)
}

func (l *ThresholdWindow[K]) makeRoomLocked() {
	for len(l.entries) >= l.maxKeys {
		var oldestKey K
		var oldest time.Time
		found := false
		for key, entry := range l.entries {
			if !found || entry.updatedAt.Before(oldest) {
				oldestKey, oldest, found = key, entry.updatedAt, true
			}
		}
		if !found {
			return
		}
		delete(l.entries, oldestKey)
	}
}
