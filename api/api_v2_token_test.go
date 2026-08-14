package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"

	"github.com/gin-gonic/gin"
)

func newAPIV2TokenTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	initSessionTestDB(t)
	if err := dbsqlite.DB().Create(&model.Tokens{
		Desc:   "legacy",
		Token:  "legacy-token",
		Expiry: 0,
		UserId: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewAPIv2Handler(router.Group("/apiv2"))
	return router
}

func performAPIV2TokenRequest(router *gin.Engine, header string, token string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/apiv2/settings", nil)
	req.Header.Set(header, token)
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestAPIV2AcceptsBearerTokenAfterHashMigration(t *testing.T) {
	router := newAPIV2TokenTestRouter(t)

	recorder := performAPIV2TokenRequest(router, "Authorization", "Bearer legacy-token")
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", recorder.Code)
	}
	var msg Msg
	if err := json.Unmarshal(recorder.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if !msg.Success {
		t.Fatalf("bearer token request failed: %s", msg.Msg)
	}
	if recorder.Header().Get("Sunset") != "" {
		t.Fatal("bearer token request should not emit legacy sunset header")
	}
}

func TestAPIV2LegacyTokenHeaderEmitsSunset(t *testing.T) {
	withAPITokenNow(t, legacyTokenHeaderSunsetAt.Add(-time.Second))
	router := newAPIV2TokenTestRouter(t)

	recorder := performAPIV2TokenRequest(router, "Token", "legacy-token")
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", recorder.Code)
	}
	var msg Msg
	if err := json.Unmarshal(recorder.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if !msg.Success {
		t.Fatalf("legacy token request failed: %s", msg.Msg)
	}
	if recorder.Header().Get("Deprecation") != "true" {
		t.Fatal("legacy token request did not emit Deprecation header")
	}
	if recorder.Header().Get("Sunset") != legacyTokenHeaderSunset {
		t.Fatalf("unexpected Sunset header: %q", recorder.Header().Get("Sunset"))
	}
}

func TestAPIV2LegacyTokenHeaderRejectedAfterSunset(t *testing.T) {
	withAPITokenNow(t, legacyTokenHeaderSunsetAt.Add(time.Second))
	router := newAPIV2TokenTestRouter(t)

	recorder := performAPIV2TokenRequest(router, "Token", "legacy-token")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if recorder.Header().Get("Deprecation") != "true" {
		t.Fatal("expired legacy token request did not emit Deprecation header")
	}
	if recorder.Header().Get("Sunset") != legacyTokenHeaderSunset {
		t.Fatalf("unexpected Sunset header: %q", recorder.Header().Get("Sunset"))
	}
	var msg Msg
	if err := json.Unmarshal(recorder.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Success {
		t.Fatal("expired legacy token request should fail")
	}
	if !strings.Contains(msg.Msg, "legacy token header expired") {
		t.Fatalf("unexpected expired legacy token message: %q", msg.Msg)
	}
}

func TestAPIV2BearerTokenAcceptedAfterLegacySunset(t *testing.T) {
	withAPITokenNow(t, legacyTokenHeaderSunsetAt.Add(time.Second))
	router := newAPIV2TokenTestRouter(t)

	recorder := performAPIV2TokenRequest(router, "Authorization", "Bearer legacy-token")
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", recorder.Code)
	}
	if recorder.Header().Get("Deprecation") != "" {
		t.Fatal("bearer token request should not emit legacy Deprecation header")
	}
	if recorder.Header().Get("Sunset") != "" {
		t.Fatal("bearer token request should not emit legacy Sunset header")
	}
	var msg Msg
	if err := json.Unmarshal(recorder.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if !msg.Success {
		t.Fatalf("bearer token request failed after legacy sunset: %s", msg.Msg)
	}
}

func TestAPIV2TokenExpiresAtExactBoundary(t *testing.T) {
	now := time.Unix(1_900_000_000, 0)
	withAPITokenNow(t, now)
	initSessionTestDB(t)
	if err := dbsqlite.DB().Create(&model.Tokens{
		Desc: "boundary", Token: "boundary-token", Expiry: now.Unix(), UserId: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewAPIv2Handler(router.Group("/apiv2"))
	recorder := performAPIV2TokenRequest(router, "Authorization", "Bearer boundary-token")
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("token at its expiry boundary returned %d, want 401", recorder.Code)
	}
}

func TestAPIV2ReloadTokensFailsClosedOnLoaderError(t *testing.T) {
	initSessionTestDB(t)
	if err := dbsqlite.DB().Create(&model.Tokens{
		Desc: "reload", Token: "reload-token", Expiry: 0, UserId: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAPIv2Handler(router.Group("/apiv2"))
	if recorder := performAPIV2TokenRequest(router, "Authorization", "Bearer reload-token"); recorder.Code != http.StatusOK {
		t.Fatalf("baseline token returned %d", recorder.Code)
	}
	handler.loadTokens = func() ([]byte, error) { return nil, errors.New("load failed") }
	handler.ReloadTokens()
	if recorder := performAPIV2TokenRequest(router, "Authorization", "Bearer reload-token"); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("stale token survived failed reload: status=%d", recorder.Code)
	}
}
