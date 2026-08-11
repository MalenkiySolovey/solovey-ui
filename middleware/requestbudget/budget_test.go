package requestbudget

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRegistryMechanicallyDeclaresEveryAdminAPIRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/app/api/read", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.POST("/app/api/login", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.POST("/app/api/importdb", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.POST("/app/api/import-xui", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.POST("/app/api/import-xui/plan", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.POST("/app/api/import-xui/apply", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.POST("/app/api/import-xui/rollback", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.POST("/app/apiv2/components/:id/apply", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.GET("/public", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	registry := NewRegistry("/app/")
	registry.DeclareGinRoutes(router.Routes())

	if got := len(registry.Policies()); got != 8 {
		t.Fatalf("declared policies=%d, want 8", got)
	}
	for _, route := range router.Routes() {
		if route.Path == "/public" {
			continue
		}
		if _, ok := registry.Lookup(route.Method, route.Path); !ok {
			t.Fatalf("route missing policy: %s %s", route.Method, route.Path)
		}
		policy, _ := registry.Lookup(route.Method, route.Path)
		if policy.Authentication == "" || policy.ActionScope == "" ||
			policy.PressureClass == "" || policy.AuditPolicy == "" ||
			policy.ResponseClass != "bounded_safe_envelope" {
			t.Fatalf("route has incomplete security inventory: %#v", policy)
		}
	}
	login, _ := registry.Lookup(http.MethodPost, "/app/api/login")
	if login.BodyClass != BodyAuthTiny || login.MaxBodyBytes != AuthTinyBytes {
		t.Fatalf("unexpected login policy: %#v", login)
	}
	database, _ := registry.Lookup(http.MethodPost, "/app/api/importdb")
	if database.BodyClass != BodyDatabase || database.MaxBodyBytes != DatabaseBytes ||
		database.ConcurrencyClass != "backup" || database.StepUpOperation != "backup.restore" {
		t.Fatalf("unexpected database policy: %#v", database)
	}
	xuiUpload, _ := registry.Lookup(http.MethodPost, "/app/api/import-xui/plan")
	if xuiUpload.BodyClass != BodyDatabase || xuiUpload.MaxBodyBytes != DatabaseBytes ||
		xuiUpload.ConcurrencyClass != "backup" || xuiUpload.ActionScope != "database_import" ||
		xuiUpload.StepUpOperation != "" {
		t.Fatalf("unexpected compatible-panel import policy: %#v", xuiUpload)
	}
	xuiApply, _ := registry.Lookup(http.MethodPost, "/app/api/import-xui/apply")
	if xuiApply.BodyClass != BodyDatabase || xuiApply.MaxBodyBytes != DatabaseBytes ||
		xuiApply.StepUpOperation != "backup.restore" {
		t.Fatalf("unexpected compatible-panel apply policy: %#v", xuiApply)
	}
	xuiRollback, _ := registry.Lookup(http.MethodPost, "/app/api/import-xui/rollback")
	if xuiRollback.BodyClass != BodyAuthTiny || xuiRollback.MaxBodyBytes != AuthTinyBytes ||
		xuiRollback.ConcurrencyClass != "backup" || xuiRollback.StepUpOperation != "backup.restore" {
		t.Fatalf("unexpected compatible-panel rollback policy: %#v", xuiRollback)
	}
	read, _ := registry.Lookup(http.MethodGet, "/app/api/read")
	if read.BodyClass != BodyNone || read.MaxBodyBytes != 0 {
		t.Fatalf("GET route must remain bodyless: %#v", read)
	}
}

func TestClassifyUsesExactBodyClassesAndHighRiskActionBindings(t *testing.T) {
	if BodyJSON != "JSON_STANDARD" || BodyConfig != "CONFIG_LARGE" ||
		BodyComponent != "COMPONENT_PACKAGE" || BodyDatabase != "DATABASE_TRANSFER" {
		t.Fatalf("request budget class names drifted: %q %q %q %q", BodyJSON, BodyConfig, BodyComponent, BodyDatabase)
	}

	remove := classify(http.MethodPost, "/api/update/components/:id/remove")
	if remove.BodyClass != BodyAuthTiny || remove.MaxBodyBytes != AuthTinyBytes ||
		remove.ActionScope != "component_remove" || remove.StepUpOperation != "drop_data" {
		t.Fatalf("component remove policy=%#v", remove)
	}
	asset := classify(http.MethodPost, "/api/components/fixture-sites/sites/:id/assets")
	if asset.BodyClass != BodyConfig || asset.MaxBodyBytes != ConfigBytes {
		t.Fatalf("asset upload policy=%#v", asset)
	}
	componentJSON := classify(http.MethodPost, "/api/components/example/apply")
	if componentJSON.BodyClass != BodyJSON || componentJSON.MaxBodyBytes != JSONBytes {
		t.Fatalf("ordinary component JSON policy=%#v", componentJSON)
	}
}

func TestMiddlewareRejectsJSONBeyondDepthLimitBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := NewRegistry("")
	router := gin.New()
	router.Use(Middleware(registry))
	reached := false
	router.POST("/api/save", func(c *gin.Context) {
		reached = true
		c.Status(http.StatusNoContent)
	})
	registry.DeclareGinRoutes(router.Routes())

	body := bytes.Repeat([]byte{'['}, MaxJSONNestingDepth+1)
	body = append(body, bytes.Repeat([]byte{']'}, MaxJSONNestingDepth+1)...)
	request := httptest.NewRequest(http.MethodPost, "/api/save", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || reached {
		t.Fatalf("deep JSON status=%d reached=%v", recorder.Code, reached)
	}
}

func TestMiddlewareReportsStableRejectionClass(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := NewRegistry("")
	var gotPolicy Policy
	var gotReason string
	router := gin.New()
	router.Use(Middleware(registry, func(_ *gin.Context, policy Policy, reason string) {
		gotPolicy = policy
		gotReason = reason
	}))
	router.POST("/api/login", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	registry.DeclareGinRoutes(router.Routes())

	request := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(make([]byte, AuthTinyBytes+1)))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize request status=%d", recorder.Code)
	}
	if gotReason != "body_limit" || gotPolicy.Route != "/api/login" || gotPolicy.AuditPolicy == "" {
		t.Fatalf("unexpected rejection report: reason=%q policy=%#v", gotReason, gotPolicy)
	}
}

func TestMiddlewareRejectsBodylessOversizeAndUnboundedPageBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := NewRegistry("")
	router := gin.New()
	router.Use(Middleware(registry))
	reached := 0
	router.GET("/api/read", func(c *gin.Context) { reached++; c.Status(http.StatusNoContent) })
	router.POST("/api/login", func(c *gin.Context) { reached++; c.Status(http.StatusNoContent) })
	registry.DeclareGinRoutes(router.Routes())

	getWithBody := httptest.NewRequest(http.MethodGet, "/api/read", bytes.NewReader([]byte("x")))
	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, getWithBody)
	if getRecorder.Code != http.StatusRequestEntityTooLarge || reached != 0 {
		t.Fatalf("bodyless request status=%d reached=%d", getRecorder.Code, reached)
	}

	oversize := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(make([]byte, AuthTinyBytes+1)))
	oversizeRecorder := httptest.NewRecorder()
	router.ServeHTTP(oversizeRecorder, oversize)
	if oversizeRecorder.Code != http.StatusRequestEntityTooLarge || reached != 0 {
		t.Fatalf("oversize request status=%d reached=%d", oversizeRecorder.Code, reached)
	}

	page := httptest.NewRequest(http.MethodGet, "/api/read?limit=201", nil)
	pageRecorder := httptest.NewRecorder()
	router.ServeHTTP(pageRecorder, page)
	if pageRecorder.Code != http.StatusBadRequest || reached != 0 {
		t.Fatalf("unbounded page status=%d reached=%d", pageRecorder.Code, reached)
	}
}

func TestMiddlewareFailsFastOnConcurrentSessionMutation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := NewRegistry("")
	router := gin.New()
	router.Use(Middleware(registry))
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	router.POST("/api/mutate", func(c *gin.Context) {
		entered <- struct{}{}
		<-release
		c.Status(http.StatusNoContent)
	})
	registry.DeclareGinRoutes(router.Routes())

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		request := httptest.NewRequest(http.MethodPost, "/api/mutate", bytes.NewReader([]byte("{}")))
		request.AddCookie(&http.Cookie{Name: "s-ui", Value: "same-session"})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		firstDone <- recorder
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first mutation did not enter handler")
	}

	second := httptest.NewRequest(http.MethodPost, "/api/mutate", bytes.NewReader([]byte("{}")))
	second.AddCookie(&http.Cookie{Name: "s-ui", Value: "same-session"})
	secondRecorder := httptest.NewRecorder()
	router.ServeHTTP(secondRecorder, second)
	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second mutation status=%d, want 429", secondRecorder.Code)
	}
	close(release)
	if recorder := <-firstDone; recorder.Code != http.StatusNoContent {
		t.Fatalf("first mutation status=%d", recorder.Code)
	}
}

func TestMiddlewareKeepsSessionLaneForGlobalComponentMutation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := NewRegistry("")
	router := gin.New()
	router.Use(Middleware(registry))
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	router.POST("/api/components/example/apply", func(c *gin.Context) {
		entered <- struct{}{}
		<-release
		c.Status(http.StatusNoContent)
	})
	registry.DeclareGinRoutes(router.Routes())
	policy, _ := registry.Lookup(http.MethodPost, "/api/components/example/apply")
	if policy.ConcurrencyClass != "component" {
		t.Fatalf("component policy=%#v", policy)
	}

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		request := httptest.NewRequest(http.MethodPost, "/api/components/example/apply", bytes.NewReader([]byte("{}")))
		request.AddCookie(&http.Cookie{Name: "s-ui", Value: "same-session"})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		firstDone <- recorder
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first component mutation did not enter handler")
	}

	second := httptest.NewRequest(http.MethodPost, "/api/components/example/apply", bytes.NewReader([]byte("{}")))
	second.AddCookie(&http.Cookie{Name: "s-ui", Value: "same-session"})
	secondRecorder := httptest.NewRecorder()
	router.ServeHTTP(secondRecorder, second)
	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second same-session component mutation status=%d, want 429", secondRecorder.Code)
	}
	close(release)
	if recorder := <-firstDone; recorder.Code != http.StatusNoContent {
		t.Fatalf("first component mutation status=%d", recorder.Code)
	}
}

func BenchmarkMiddlewareBudgetAdmission(b *testing.B) {
	gin.SetMode(gin.TestMode)
	registry := NewRegistry("")
	router := gin.New()
	router.Use(Middleware(registry))
	router.POST("/api/v1/security/check", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	registry.DeclareGinRoutes(router.Routes())

	b.ReportAllocs()
	for range b.N {
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/security/check",
			bytes.NewReader([]byte(`{"value":"bounded"}`)),
		)
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(&http.Cookie{Name: "s-ui", Value: "benchmark-session"})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNoContent {
			b.Fatalf("status=%d", recorder.Code)
		}
	}
}
