//go:build !minimal

package telegramapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	telegramservice "github.com/MalenkiySolovey/solovey-ui/components/telegram/service"
	"github.com/gin-gonic/gin"
)

func TestRoutesFailClosedWhenHostDependenciesAreMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api")
	RegisterRoutes(api, Deps{})

	for _, path := range []string{"/api/telegram/test", "/api/telegram/detect-chat", "/api/telegram/backup", "/api/telegram/backup/run"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, nil)
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d, want %d; body=%s", path, recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
		}
	}
}

func TestManualBackupFailsClosedBeforeWorkWithoutNarrowAuditRecorder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	rateCalls := 0
	RegisterRoutes(router.Group("/api"), Deps{
		Telegram:       &telegramservice.Service{Settings: discoveryReader{}},
		RequireScope:   func(*gin.Context, string, ...string) bool { return true },
		Actor:          func(*gin.Context) string { return "admin" },
		RemoteIP:       func(*gin.Context) string { return "192.0.2.1" },
		CheckRateLimit: func(string) (time.Duration, error) { rateCalls++; return 0, nil },
		Audit: func(*gin.Context, string, string, string, string, map[string]any) {
			t.Fatal("backup work must not begin")
		},
		JSONObj: func(*gin.Context, interface{}, error) { t.Fatal("unaudited backup must not succeed") },
	})
	for _, path := range []string{"/api/telegram/backup", "/api/telegram/backup/run"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != http.StatusServiceUnavailable || rateCalls != 0 {
			t.Fatalf("missing recorder did not fail before work: status=%d rateCalls=%d", response.Code, rateCalls)
		}
	}
}

func TestBackupHTTPStatusMapping(t *testing.T) {
	tests := []struct {
		errorClass string
		want       int
	}{
		{"concurrent_run", http.StatusConflict},
		{"rate_limited", http.StatusTooManyRequests},
		{"disabled", http.StatusServiceUnavailable},
		{"missing_token", http.StatusServiceUnavailable},
		{"missing_chat", http.StatusServiceUnavailable},
		{"missing_passphrase", http.StatusServiceUnavailable},
		{"oversize", http.StatusServiceUnavailable},
		{"network", http.StatusServiceUnavailable},
		{"proxy", http.StatusServiceUnavailable},
		{"unauthorized", http.StatusServiceUnavailable},
		{"chat_not_found", http.StatusServiceUnavailable},
		{"db_snapshot_failed", http.StatusInternalServerError},
		{"encryption_failed", http.StatusInternalServerError},
		{"settings", http.StatusInternalServerError},
		{"payload", http.StatusInternalServerError},
		{"request", http.StatusInternalServerError},
		{"unknown", http.StatusInternalServerError},
		{"internal", http.StatusInternalServerError},
		{"new_telegram_class", http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.errorClass, func(t *testing.T) {
			if got := BackupHTTPStatus(tt.errorClass); got != tt.want {
				t.Fatalf("BackupHTTPStatus(%q)=%d, want %d", tt.errorClass, got, tt.want)
			}
		})
	}
}
