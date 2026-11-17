package realtimehttp

import (
	"strconv"
	"testing"
	"time"
)

func TestSecurityConsumeWSTokenDoubleSpendExpiredAndCapacity(t *testing.T) {
	_ = ResetTokens()
	t.Cleanup(func() { _ = ResetTokens() })

	StoreToken("single-use", "admin", time.Now().Add(time.Minute))
	if user, ok := ConsumeToken("single-use"); !ok || user != "admin" {
		t.Fatalf("first consume failed: user=%q ok=%v", user, ok)
	}
	if user, ok := ConsumeToken("single-use"); ok || user != "" {
		t.Fatalf("second consume should fail: user=%q ok=%v", user, ok)
	}

	StoreToken("expired", "admin", time.Now().Add(-time.Second))
	if user, ok := ConsumeToken("expired"); ok || user != "" {
		t.Fatalf("expired consume should fail: user=%q ok=%v", user, ok)
	}
	if HasToken("expired") {
		t.Fatal("expired matched token should be deleted after consume attempt")
	}

	StoreToken("oldest", "admin", time.Now().Add(time.Minute))
	for i := 0; i < MaxTokens; i++ {
		token := "new-" + strconv.Itoa(i)
		StoreToken(token, "admin", time.Now().Add(time.Hour+time.Duration(i)*time.Second))
	}
	if HasToken("oldest") {
		t.Fatal("oldest websocket token was not evicted when capacity was exceeded")
	}
	if size := TokenCount(); size != MaxTokens {
		t.Fatalf("unexpected websocket token capacity size %d, want %d", size, MaxTokens)
	}
}

func TestSecurityWSTokenSweepExpiresExactBoundary(t *testing.T) {
	_ = ResetTokens()
	t.Cleanup(func() { _ = ResetTokens() })
	expiresAt := time.Now().Add(time.Hour)
	StoreToken("boundary", "admin", expiresAt)
	SweepExpired(expiresAt)
	if HasToken("boundary") {
		t.Fatal("websocket token survived its exact expiry boundary")
	}
}
