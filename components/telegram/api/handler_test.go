//go:build !minimal

package telegramapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRoutesFailClosedWhenHostDependenciesAreMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	api := router.Group("/api")
	RegisterRoutes(api, Deps{})

	for _, path := range []string{"/api/telegram/test", "/api/telegram/backup", "/api/telegram/backup/run"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, nil)
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d, want %d; body=%s", path, recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
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
