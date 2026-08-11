package state

import "testing"

func TestActiveCacheDoesNotPublishAcrossInvalidation(t *testing.T) {
	activeCache.Lock()
	activeCache.ids = nil
	activeCache.generation = 41
	activeCache.Unlock()
	t.Cleanup(func() {
		activeCache.Lock()
		activeCache.ids = nil
		activeCache.generation++
		activeCache.Unlock()
	})

	InvalidateActiveCache()
	if _, published := publishActiveIDs(41, map[string]struct{}{"stale": {}}); published {
		t.Fatal("snapshot loaded before invalidation was published")
	}

	activeCache.RLock()
	generation := activeCache.generation
	activeCache.RUnlock()
	cached, published := publishActiveIDs(generation, map[string]struct{}{"fresh": {}})
	if !published {
		t.Fatal("current-generation snapshot was rejected")
	}
	if _, ok := cached["fresh"]; !ok {
		t.Fatalf("current-generation snapshot = %#v, want fresh id", cached)
	}
	if _, ok := cached["stale"]; ok {
		t.Fatalf("stale id survived invalidation: %#v", cached)
	}
}
