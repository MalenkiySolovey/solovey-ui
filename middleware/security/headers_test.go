package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// Inject a checker that reports the request as secure → HSTS expected.
	router.Use(Admin(func(*gin.Context) bool { return true }))
	router.GET("/", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	router.ServeHTTP(recorder, req)

	headers := recorder.Result().Header
	if headers.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing nosniff header: %#v", headers)
	}
	if headers.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("missing admin frame denial: %#v", headers)
	}
	if headers.Get("Referrer-Policy") != "strict-origin-when-cross-origin" {
		t.Fatalf("unexpected referrer policy: %#v", headers)
	}
	if !strings.Contains(headers.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatalf("unexpected CSP: %q", headers.Get("Content-Security-Policy"))
	}
	csp := headers.Get("Content-Security-Policy")
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Fatalf("script-src should not allow unsafe-inline: %q", csp)
	}
	if !strings.Contains(csp, "style-src 'self' 'unsafe-inline'") {
		t.Fatalf("style-src should keep unsafe-inline for Vuetify: %q", csp)
	}
	if headers.Get("Strict-Transport-Security") == "" {
		t.Fatal("HSTS should be set for secure requests")
	}
}

func TestAdminSkipHSTSForPlainHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// Injected checker reports not-secure → HSTS must be skipped.
	router.Use(Admin(func(*gin.Context) bool { return false }))
	router.GET("/", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(recorder, req)

	if recorder.Result().Header.Get("Strict-Transport-Security") != "" {
		t.Fatal("HSTS should not be set for plain HTTP requests")
	}
}

func TestSubscriptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Subscriptions())
	router.GET("/", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(recorder, req)

	headers := recorder.Result().Header
	if headers.Get("Cache-Control") != "no-store" {
		t.Fatalf("missing no-store cache header: %#v", headers)
	}
	if headers.Get("X-Frame-Options") != "" {
		t.Fatalf("sub server should not set X-Frame-Options: %#v", headers)
	}
	if headers.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing nosniff header: %#v", headers)
	}
}
