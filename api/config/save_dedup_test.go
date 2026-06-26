package config

import (
	"testing"
	"time"
)

func TestSaveDedupInFlightOutlastsWindow(t *testing.T) {
	cache := &saveDedupCache{seen: make(map[string]dedupEntry)}
	const key = "key"
	if !cache.claim(key, 0) {
		t.Fatal("first claim should succeed")
	}
	if cache.claim(key, int64(30*time.Second)) {
		t.Fatal("in-flight identical claim must be deduped regardless of elapsed time")
	}
	cache.complete(key, int64(30*time.Second))
	if cache.claim(key, int64(31*time.Second)) {
		t.Fatal("within post-completion window must be deduped")
	}
	if !cache.claim(key, int64(30*time.Second)+int64(saveDedupWindow)+int64(time.Second)) {
		t.Fatal("after the post-completion window the claim should succeed")
	}
}

func TestSaveDedupReleaseAllowsImmediateRetry(t *testing.T) {
	cache := &saveDedupCache{seen: make(map[string]dedupEntry)}
	if !cache.claim("key", 0) {
		t.Fatal("claim should succeed")
	}
	cache.release("key")
	if !cache.claim("key", 1) {
		t.Fatal("release must allow an immediate retry of a failed save")
	}
}

func TestSaveDedupStuckInFlightEvicted(t *testing.T) {
	cache := &saveDedupCache{seen: make(map[string]dedupEntry)}
	if !cache.claim("key", 0) {
		t.Fatal("claim should succeed")
	}
	if cache.claim("key", int64(saveDedupMaxInFlight)/2) {
		t.Fatal("in-flight within the safety cap must dedup")
	}
	if !cache.claim("key", int64(saveDedupMaxInFlight)+1) {
		t.Fatal("a stuck in-flight entry past the cap must be evicted")
	}
}
