package sub

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	subserver "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/server"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func TestRateLimitMiddlewareCanonicalizesMappedClientIP(t *testing.T) {
	initSubTestDB(t)
	subserver.ResetRateLimitForTest()
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", "subRateLimitPerIP").Update("value", "2").Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(subserver.RateLimitMiddleware())
	router.GET("/sub/:subid", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	for _, remoteAddr := range []string{"198.51.100.10:12345", "[::ffff:198.51.100.10]:12345"} {
		recorder := performRateLimitRequestWithRemoteAddr(router, remoteAddr)
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("request from %s should pass, got %d", remoteAddr, recorder.Code)
		}
	}
	recorder := performRateLimitRequestWithRemoteAddr(router, "198.51.100.10:54321")
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("canonical mapped client should share bucket, got %d", recorder.Code)
	}
}

func TestRateLimitMiddlewareUsesConfiguredLimitAndRetryAfter(t *testing.T) {
	initSubTestDB(t)
	subserver.ResetRateLimitForTest()
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", "subRateLimitPerIP").Update("value", "2").Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(subserver.RateLimitMiddleware())
	router.GET("/sub/:subid", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	for i := 0; i < 2; i++ {
		recorder := performRateLimitRequest(router)
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("request %d should pass, got %d", i, recorder.Code)
		}
	}
	recorder := performRateLimitRequest(router)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("third request should be rate-limited, got %d", recorder.Code)
	}
	if recorder.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}
}

func performRateLimitRequest(router *gin.Engine) *httptest.ResponseRecorder {
	return performRateLimitRequestWithRemoteAddr(router, "198.51.100.10:12345")
}

func performRateLimitRequestWithRemoteAddr(router *gin.Engine, remoteAddr string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sub/alice", nil)
	req.RemoteAddr = remoteAddr
	router.ServeHTTP(recorder, req)
	return recorder
}
