//go:build !minimal

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	importxuihttp "github.com/MalenkiySolovey/solovey-ui/components/import-xui/api"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func BenchmarkAPI_ImportXUIReports(b *testing.B) {
	router := newAPIPerfRouter(b)
	addImportXUIReportsPerfRoute(router)
	b.ReportMetric(float64(importxuihttp.RequestLimit), "rate_limit")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		importxuihttp.ResetRateLimits()
		for j := 0; j < importxuihttp.RequestLimit; j++ {
			req := httptest.NewRequest(http.MethodGet, "/import-xui/reports", nil)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusOK {
				b.Fatalf("GET /import-xui/reports status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		}
	}
}

func TestAPIImportXUIReportsRateLimitPhase5(t *testing.T) {
	router := newAPIPerfRouter(t)
	addImportXUIReportsPerfRoute(router)
	importxuihttp.ResetRateLimits()
	statuses := runAPIPerfLoad(t, router, http.MethodGet, "/import-xui/reports", "", 1, 100)
	if statuses[http.StatusOK] != importxuihttp.RequestLimit || statuses[http.StatusTooManyRequests] != 100-importxuihttp.RequestLimit {
		t.Fatalf("unexpected rate-limit statuses=%v want ok=%d too_many=%d", statuses, importxuihttp.RequestLimit, 100-importxuihttp.RequestLimit)
	}
	t.Logf("phase5 issue36/44 anchor: GET /import-xui/reports requests=100 rate_limit=%d statuses=%v", importxuihttp.RequestLimit, statuses)
}

func addImportXUIReportsPerfRoute(router *gin.Engine) {
	runtime := service.NewRuntime(nil)
	apiService := NewApiService(WithRuntime(runtime))
	importHandler := apiService.importXUIHandler()
	router.GET("/import-xui/reports", withTestTokenScope("admin", "admin", importHandler.ImportXuiReports))
}
