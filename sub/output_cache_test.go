package sub

import (
	"testing"
	"time"
)

func TestSubscriptionOutputCacheExpiresAndCopiesHeaders(t *testing.T) {
	oldCache := subscriptionOutputCache
	subscriptionOutputCache = newSubscriptionOutputCache(time.Minute)
	t.Cleanup(func() { subscriptionOutputCache = oldCache })

	now := time.Unix(100, 0)
	headers := []string{"info", "interval", "title"}
	subscriptionCacheSet("base:test", "body", headers, now)
	headers[0] = "mutated"

	body, gotHeaders, ok := subscriptionCacheGet("base:test", now.Add(30*time.Second))
	if !ok {
		t.Fatal("expected cache hit")
	}
	if body != "body" {
		t.Fatalf("unexpected cached body: %q", body)
	}
	if gotHeaders[0] != "info" {
		t.Fatalf("headers were not copied on set: %#v", gotHeaders)
	}

	gotHeaders[0] = "mutated again"
	_, gotHeaders, ok = subscriptionCacheGet("base:test", now.Add(31*time.Second))
	if !ok {
		t.Fatal("expected second cache hit")
	}
	if gotHeaders[0] != "info" {
		t.Fatalf("headers were not copied on get: %#v", gotHeaders)
	}

	if _, _, ok = subscriptionCacheGet("base:test", now.Add(61*time.Second)); ok {
		t.Fatal("expected cache entry to expire")
	}
}

func TestClearSubscriptionOutputCache(t *testing.T) {
	oldCache := subscriptionOutputCache
	subscriptionOutputCache = newSubscriptionOutputCache(time.Minute)
	t.Cleanup(func() { subscriptionOutputCache = oldCache })

	now := time.Unix(100, 0)
	subscriptionCacheSet("json:test", "body", []string{"info"}, now)
	ClearSubscriptionOutputCache()

	if _, _, ok := subscriptionCacheGet("json:test", now); ok {
		t.Fatal("expected cache miss after clear")
	}
}
