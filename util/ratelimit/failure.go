package ratelimit

import (
	"sync"
	"time"
)

type failureEntry struct {
	failures     int
	firstFailure time.Time
	blockedUntil time.Time
	updatedAt    time.Time
}

type FailureWindow[K comparable] struct {
	mu         sync.Mutex
	entries    map[K]failureEntry
	window     time.Duration
	threshold  int
	blockFor   time.Duration
	maxKeys    int
	gcEvery    time.Duration
	lastGC     time.Time
	tarpitStep time.Duration
	tarpitMax  time.Duration
	janitor    *janitor
}

func NewFailureWindow[K comparable](
	window time.Duration,
	threshold int,
	blockFor time.Duration,
	maxKeys int,
	gcEvery time.Duration,
	tarpitStep time.Duration,
	tarpitMax time.Duration,
) *FailureWindow[K] {
	if window <= 0 || threshold <= 0 || blockFor <= 0 || maxKeys <= 0 {
		panic("ratelimit: invalid failure window configuration")
	}
	l := &FailureWindow[K]{
		entries:    make(map[K]failureEntry),
		window:     window,
		threshold:  threshold,
		blockFor:   blockFor,
		maxKeys:    maxKeys,
		gcEvery:    gcEvery,
		tarpitStep: tarpitStep,
		tarpitMax:  tarpitMax,
	}
	l.janitor = startJanitor(gcEvery, l.PruneAt)
	return l
}

func (l *FailureWindow[K]) RecordFailure(key K) int {
	return l.RecordFailureAt(key, time.Now())
}

func (l *FailureWindow[K]) RecordFailureAt(key K, now time.Time) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maybePruneLocked(now)
	entry, exists := l.entries[key]
	if !exists {
		l.makeRoomLocked()
	}
	if entry.firstFailure.IsZero() || now.Sub(entry.firstFailure) > l.window {
		entry = failureEntry{firstFailure: now}
	}
	entry.failures++
	entry.updatedAt = now
	if entry.failures >= l.threshold {
		entry.blockedUntil = now.Add(l.blockFor)
	}
	l.entries[key] = entry
	return entry.failures
}

func (l *FailureWindow[K]) Blocked(key K) Decision {
	return l.BlockedAt(key, time.Now())
}

func (l *FailureWindow[K]) BlockedAt(key K, now time.Time) Decision {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maybePruneLocked(now)
	entry, exists := l.entries[key]
	if exists && entryExpired(entry, now, l.window) {
		delete(l.entries, key)
		exists = false
	}
	if !exists || entry.blockedUntil.IsZero() || !now.Before(entry.blockedUntil) {
		return Decision{Allowed: true, Count: entry.failures}
	}
	return Decision{RetryAfter: entry.blockedUntil.Sub(now), Count: entry.failures}
}

func (l *FailureWindow[K]) TarpitDelay(key K) time.Duration {
	return l.TarpitDelayAt(key, time.Now())
}

func (l *FailureWindow[K]) TarpitDelayAt(key K, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maybePruneLocked(now)
	entry, exists := l.entries[key]
	if exists && entryExpired(entry, now, l.window) {
		delete(l.entries, key)
		exists = false
	}
	if !exists || entry.failures < l.threshold || l.tarpitStep <= 0 {
		return 0
	}
	over := entry.failures - l.threshold + 1
	delay := time.Duration(over) * l.tarpitStep
	if l.tarpitMax > 0 && delay > l.tarpitMax {
		return l.tarpitMax
	}
	return delay
}

func (l *FailureWindow[K]) Reset(key K) {
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

func (l *FailureWindow[K]) ResetAll() {
	l.mu.Lock()
	l.entries = make(map[K]failureEntry)
	l.mu.Unlock()
}

func (l *FailureWindow[K]) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

func (l *FailureWindow[K]) PruneAt(now time.Time) {
	l.mu.Lock()
	l.pruneLocked(now)
	l.mu.Unlock()
}

func (l *FailureWindow[K]) Close() {
	l.janitor.close()
}

func (l *FailureWindow[K]) pruneLocked(now time.Time) {
	for key, entry := range l.entries {
		if entryExpired(entry, now, l.window) {
			delete(l.entries, key)
		}
	}
	l.lastGC = now
}

func (l *FailureWindow[K]) maybePruneLocked(now time.Time) {
	if len(l.entries) < l.maxKeys && (l.gcEvery <= 0 || (!l.lastGC.IsZero() && now.Sub(l.lastGC) < l.gcEvery)) {
		return
	}
	l.pruneLocked(now)
}

func entryExpired(entry failureEntry, now time.Time, window time.Duration) bool {
	return (entry.blockedUntil.IsZero() || !now.Before(entry.blockedUntil)) &&
		(entry.firstFailure.IsZero() || now.Sub(entry.firstFailure) >= window)
}

func (l *FailureWindow[K]) makeRoomLocked() {
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
