package ratelimit

import (
	"sync"
	"time"
)

type slidingEntry struct {
	timestamps []time.Time
	updatedAt  time.Time
}

type SlidingWindow[K comparable] struct {
	mu      sync.Mutex
	entries map[K]slidingEntry
	window  time.Duration
	limit   int
	maxKeys int
	gcEvery time.Duration
	lastGC  time.Time
	janitor *janitor
}

func NewSlidingWindow[K comparable](window time.Duration, limit, maxKeys int, gcEvery time.Duration) *SlidingWindow[K] {
	if window <= 0 {
		panic("ratelimit: sliding window must be positive")
	}
	if maxKeys <= 0 {
		panic("ratelimit: max keys must be positive")
	}
	l := &SlidingWindow[K]{entries: make(map[K]slidingEntry), window: window, limit: limit, maxKeys: maxKeys, gcEvery: gcEvery}
	l.janitor = startJanitor(gcEvery, l.PruneAt)
	return l
}

func (l *SlidingWindow[K]) Allow(key K) Decision {
	return l.AllowAt(key, time.Now())
}

func (l *SlidingWindow[K]) AllowAt(key K, now time.Time) Decision {
	return l.AllowWithLimitAt(key, l.limit, now)
}

func (l *SlidingWindow[K]) AllowWithLimit(key K, limit int) Decision {
	return l.AllowWithLimitAt(key, limit, time.Now())
}

func (l *SlidingWindow[K]) AllowWithLimitAt(key K, limit int, now time.Time) Decision {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maybePruneLocked(now)
	entry, exists := l.entries[key]
	if !exists {
		l.makeRoomLocked()
	}
	entry.timestamps = pruneTimestamps(entry.timestamps, now.Add(-l.window))
	entry.updatedAt = now
	if limit <= 0 || len(entry.timestamps) >= limit {
		l.entries[key] = entry
		deadline := now.Add(l.window)
		if len(entry.timestamps) > 0 {
			deadline = entry.timestamps[0].Add(l.window)
		}
		return Decision{RetryAfter: retryAfter(deadline, now, l.window), Count: len(entry.timestamps)}
	}
	entry.timestamps = append(entry.timestamps, now)
	l.entries[key] = entry
	return Decision{Allowed: true, Count: len(entry.timestamps)}
}

func (l *SlidingWindow[K]) Reset(key K) {
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

func (l *SlidingWindow[K]) ResetAll() {
	l.mu.Lock()
	l.entries = make(map[K]slidingEntry)
	l.mu.Unlock()
}

func (l *SlidingWindow[K]) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

func (l *SlidingWindow[K]) PruneAt(now time.Time) {
	l.mu.Lock()
	l.pruneLocked(now)
	l.mu.Unlock()
}

func (l *SlidingWindow[K]) Close() {
	l.janitor.close()
}

func (l *SlidingWindow[K]) pruneLocked(now time.Time) {
	cutoff := now.Add(-l.window)
	for key, entry := range l.entries {
		entry.timestamps = pruneTimestamps(entry.timestamps, cutoff)
		if len(entry.timestamps) == 0 {
			delete(l.entries, key)
			continue
		}
		l.entries[key] = entry
	}
	l.lastGC = now
}

func (l *SlidingWindow[K]) maybePruneLocked(now time.Time) {
	if len(l.entries) < l.maxKeys && (l.gcEvery <= 0 || (!l.lastGC.IsZero() && now.Sub(l.lastGC) < l.gcEvery)) {
		return
	}
	l.pruneLocked(now)
}

func (l *SlidingWindow[K]) makeRoomLocked() {
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

func pruneTimestamps(timestamps []time.Time, cutoff time.Time) []time.Time {
	first := 0
	for first < len(timestamps) && !timestamps[first].After(cutoff) {
		first++
	}
	if first == 0 {
		return timestamps
	}
	return append([]time.Time(nil), timestamps[first:]...)
}
