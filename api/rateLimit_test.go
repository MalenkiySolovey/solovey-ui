package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	realtimehttp "github.com/MalenkiySolovey/solovey-ui/api/realtime"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"

	"github.com/gin-gonic/gin"
)

func resetRateLimitState() {
	loginRateLimiter.ResetAll()
	realtimehttp.ResetRateLimits()
	auditEndpointRateLimiter.ResetAll()
	telegramBackupManualRateLimiter.ResetAll()
}

func TestLoginRateLimitBlocksAfterMaxFailures(t *testing.T) {
	resetRateLimitState()
	key := "1.2.3.4"
	for i := 0; i < loginRateLimitMax; i++ {
		if err := checkLoginRateLimit(key); err != nil {
			t.Fatalf("attempt %d should not be blocked yet: %v", i, err)
		}
		recordLoginFailure(key)
	}
	err := checkLoginRateLimit(key)
	if err == nil || !strings.Contains(err.Error(), "too many login attempts") {
		t.Fatalf("expected key to be blocked after %d failures, got %v", loginRateLimitMax, err)
	}
}

func TestLoginRateLimitResetClearsState(t *testing.T) {
	resetRateLimitState()
	key := "5.6.7.8"
	for i := 0; i < loginRateLimitMax; i++ {
		recordLoginFailure(key)
	}
	if err := checkLoginRateLimit(key); err == nil {
		t.Fatal("expected key to be blocked")
	}
	resetLoginFailures(key)
	if err := checkLoginRateLimit(key); err != nil {
		t.Fatalf("expected key to be unblocked after reset, got %v", err)
	}
}

func TestLoginRateLimitConcurrent(t *testing.T) {
	resetRateLimitState()
	const goroutines = 64
	const perGoroutine = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			key := "10.0.0." + string(rune('0'+g%10))
			for i := 0; i < perGoroutine; i++ {
				_ = checkLoginRateLimit(key)
				recordLoginFailure(key)
				if i%loginRateLimitMax == 0 {
					resetLoginFailures(key)
				}
			}
		}(g)
	}
	wg.Wait()
}

func TestWSHandshakeRateLimitBlocksAfterMaxAttemptsPerEndpointAndIP(t *testing.T) {
	resetRateLimitState()
	key := realtimehttp.HandshakeRateLimitKey("ws", "198.51.100.10")
	for i := 0; i < realtimehttp.HandshakeLimit; i++ {
		if err := realtimehttp.CheckHandshakeRateLimit(key); err != nil {
			t.Fatalf("attempt %d should not be blocked: %v", i, err)
		}
	}
	if err := realtimehttp.CheckHandshakeRateLimit(key); err == nil || !strings.Contains(err.Error(), "too many websocket handshake attempts") {
		t.Fatalf("expected rate-limit error, got %v", err)
	}
	if err := realtimehttp.CheckHandshakeRateLimit(realtimehttp.HandshakeRateLimitKey("ws-token", "198.51.100.10")); err != nil {
		t.Fatalf("separate endpoint bucket should not be blocked: %v", err)
	}
	if err := realtimehttp.CheckHandshakeRateLimit(realtimehttp.HandshakeRateLimitKey("ws", "198.51.100.11")); err != nil {
		t.Fatalf("separate IP bucket should not be blocked: %v", err)
	}
}

func TestEnforceWSHandshakeRateLimitReturns429AndAudits(t *testing.T) {
	resetRateLimitState()
	initSessionTestDB(t)
	key := realtimehttp.HandshakeRateLimitKey("ws-token", "198.51.100.10")
	for i := 0; i < realtimehttp.HandshakeLimit; i++ {
		if err := realtimehttp.CheckHandshakeRateLimit(key); err != nil {
			t.Fatal(err)
		}
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/api/realtime/ws-token", nil)
	req.RemoteAddr = "198.51.100.10:1234"
	c.Request = req

	if (&ApiService{}).realtimeHandler().EnforceHandshakeRateLimit(c, "ws-token") {
		t.Fatal("expected request to be rate-limited")
	}
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("unexpected status %d", recorder.Code)
	}
	if recorder.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After header was not set")
	}
	var msg Msg
	if err := json.Unmarshal(recorder.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Success || !strings.Contains(msg.Msg, "too many websocket handshake attempts") {
		t.Fatalf("unexpected JSON response: %#v", msg)
	}

	flushAPIAudit(t)

	var event model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "ws_rate_limited").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	var details map[string]any
	if err := json.Unmarshal(event.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details["endpoint"] != "ws-token" {
		t.Fatalf("unexpected audit details: %#v", details)
	}
	if _, ok := details["token"]; ok {
		t.Fatalf("token leaked into audit details: %#v", details)
	}
}
