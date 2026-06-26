package importxui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestXUIRateLimitUniqueIPFloodBoundedIssue36(t *testing.T) {
	ResetRateLimits()
	t.Cleanup(ResetRateLimits)

	router := newXUIRateLimitRouterIssue36()
	for i := 0; i < xuiRateMaxEntries+256; i++ {
		if status := performXUIRateLimitRequestIssue36(router, issue36RemoteAddr(i)); status != http.StatusNoContent {
			t.Fatalf("first request for unique remote addr %d returned status %d", i, status)
		}
	}

	if got := xuiRequestRateLimiter.Len(); got > xuiRateMaxEntries {
		t.Fatalf("xui rate-limit cache length = %d, want <= %d", got, xuiRateMaxEntries)
	}
}

func TestXUIRateLimitPrunesExpiredBucketsIssue36(t *testing.T) {
	ResetRateLimits()
	t.Cleanup(ResetRateLimits)

	staleAt := time.Now().Add(-2 * xuiRequestWindow)
	for i := 0; i < xuiRateMaxEntries; i++ {
		xuiRequestRateLimiter.AllowAt(fmt.Sprintf("stale-%d", i), staleAt)
	}

	router := newXUIRateLimitRouterIssue36()
	if status := performXUIRateLimitRequestIssue36(router, "10.250.0.1:1234"); status != http.StatusNoContent {
		t.Fatalf("new request after stale cache seed returned status %d", status)
	}

	if got := xuiRequestRateLimiter.Len(); got != 1 {
		t.Fatalf("xui rate-limit cache length = %d, want only the fresh bucket", got)
	}
}

func newXUIRateLimitRouterIssue36() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := &Handler{
		Actor:    func(*gin.Context) string { return "" },
		RemoteIP: func(c *gin.Context) string { return c.ClientIP() },
		Audit:    func(*gin.Context, string, string, string, string, map[string]any) {},
	}
	router.GET("/api/import-xui/reports", func(c *gin.Context) {
		if !handler.enforceRateLimit(c) {
			return
		}
		c.Status(http.StatusNoContent)
	})
	return router
}

func performXUIRateLimitRequestIssue36(router *gin.Engine, remoteAddr string) int {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/import-xui/reports", nil)
	req.RemoteAddr = remoteAddr
	router.ServeHTTP(recorder, req)
	return recorder.Code
}

func issue36RemoteAddr(i int) string {
	return fmt.Sprintf("10.%d.%d.%d:1234", (i>>16)&255, (i>>8)&255, i&255)
}
