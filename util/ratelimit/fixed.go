package ratelimit

import (
	"sync"
	"time"
)

type fixedEntry struct {
	windowStart time.Time
	updatedAt   time.Time
	count       int
}

type FixedWindow[K comparable] struct {
	mu      sync.Mutex
	entries map[K]fixedEntry
	window  time.Duration
	limit   int
	maxKeys int
	gcEvery time.Duration
	lastGC  time.Time
	janitor *janitor
}

func NewFixedWindow[K comparable](window time.Duration, limit, maxKeys int, gcEvery time.Duration) *FixedWindow[K] {
	if window <= 0 {
		panic("ratelimit: fixed window must be positive")
	}
	if maxKeys <= 0 {
		panic("ratelimit: max keys must be positive")
	}
	l := &FixedWindow[K]{entries: make(map[K]fixedEntry), window: window, limit: limit, maxKeys: maxKeys, gcEvery: gcEvery}
	l.janitor = startJanitor(gcEvery, l.PruneAt)
	return l
}

func (l *FixedWindow[K]) Allow(key K) Decision {
	return l.AllowAt(key, time.Now())
}

func (l *FixedWindow[K]) AllowAt(key K, now time.Time) Decision {
	return l.AllowWithLimitAt(key, l.limit, now)
}

func (l *FixedWindow[K]) AllowWithLimit(key K, limit int) Decision {
	return l.AllowWithLimitAt(key, limit, time.Now())
}

func (l *FixedWindow[K]) AllowWithLimitAt(key K, limit int, now time.Time) Decision {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maybePruneLocked(now)
	entry, exists := l.entries[key]
	if !exists {
		l.makeRoomLocked()
		entry = fixedEntry{windowStart: now}
	} else if now.Sub(entry.windowStart) >= l.window {
		entry = fixedEntry{windowStart: now}
	}
	entry.updatedAt = now
	if limit <= 0 || entry.count >= limit {
		l.entries[key] = entry
		return Decision{RetryAfter: retryAfter(entry.windowStart.Add(l.window), now, l.window), Count: entry.count}
	}
	entry.count++
	l.entries[key] = entry
	return Decision{Allowed: true, Count: entry.count}
}

func (l *FixedWindow[K]) Reset(key K) {
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

func (l *FixedWindow[K]) ResetAll() {
	l.mu.Lock()
	l.entries = make(map[K]fixedEntry)
	l.mu.Unlock()
}

func (l *FixedWindow[K]) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

func (l *FixedWindow[K]) PruneAt(now time.Time) {
	l.mu.Lock()
	l.pruneLocked(now)
	l.mu.Unlock()
}

func (l *FixedWindow[K]) Close() {
	l.janitor.close()
}

func (l *FixedWindow[K]) pruneLocked(now time.Time) {
	for key, entry := range l.entries {
		if now.Sub(entry.windowStart) >= l.window {
			delete(l.entries, key)
		}
	}
	l.lastGC = now
}

func (l *FixedWindow[K]) maybePruneLocked(now time.Time) {
	if len(l.entries) < l.maxKeys && (l.gcEvery <= 0 || (!l.lastGC.IsZero() && now.Sub(l.lastGC) < l.gcEvery)) {
		return
	}
	l.pruneLocked(now)
}

func (l *FixedWindow[K]) makeRoomLocked() {
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
