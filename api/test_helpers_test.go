package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/migration"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func newAuthenticatedTestRouter(t *testing.T, settingService *service.SettingService, register func(*gin.Engine)) (*gin.Engine, []*http.Cookie) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(sessions.Sessions("s-ui", cookie.NewStore([]byte("test-secret"))))
	router.GET("/login", func(c *gin.Context) {
		generation, err := settingService.GetSessionGeneration()
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		if err := SetLoginUser(c, "admin", 0, generation); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})
	if register != nil {
		register(router)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/login", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("login returned %d", recorder.Code)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login did not set a session cookie")
	}
	return router, cookies
}

func performAuthenticatedTestRequest(router *gin.Engine, req *http.Request, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	if csrfProtectedMethod(req.Method) && req.Header.Get("Origin") == "" {
		req.Header.Set("Origin", "http://example.com")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	router.ServeHTTP(recorder, req)
	_ = service.StopAuditWriter(context.Background())
	return recorder
}

func flushAPIAudit(t testing.TB) {
	t.Helper()
	if err := service.StopAuditWriter(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func withTestTokenScope(username string, scope string, handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(apiUsernameKey, username)
		c.Set(apiTokenScopeKey, scope)
		handler(c)
	}
}

func stopAPITestBackgroundWriters(tb testing.TB) {
	tb.Helper()
	if err := service.StopTokenUseDebouncer(context.Background()); err != nil {
		tb.Logf("token use debouncer stop before test DB handoff failed: %v", err)
	}
	if err := service.StopAuditWriter(context.Background()); err != nil {
		tb.Logf("audit writer stop before test DB handoff failed: %v", err)
	}
}

func closeAPITestDB(tb testing.TB) {
	tb.Helper()
	stopAPITestBackgroundWriters(tb)
	if err := dbsqlite.Close(); err != nil {
		tb.Logf("close API test database: %v", err)
	}
}

func initAPITestDB(tb testing.TB, dbPath string) {
	tb.Helper()
	// The audit writer and token-use debouncer resolve the global database at
	// flush time. Quiesce both before detaching the previous test database so a
	// delayed write cannot lock or contaminate the next test database.
	closeAPITestDB(tb)
	if err := dbsqlite.Init(dbPath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			tb.Skip(err)
		}
		tb.Fatal(err)
	}
	if err := migration.EnsureCurrentSchemaJournal(dbsqlite.DB(), true); err != nil {
		tb.Fatal(err)
	}
}

func TestAPITestDBHandoffQuiescesBackgroundWriters(t *testing.T) {
	firstPath := filepath.Join(t.TempDir(), "first.db")
	initAPITestDB(t, firstPath)
	if err := (&service.AuditService{}).Record(service.AuditEvent{
		Actor: "test", Event: "handoff_pending", Resource: "database",
	}); err != nil {
		t.Fatal(err)
	}
	closeAPITestDB(t)

	secondPath := filepath.Join(t.TempDir(), "second.db")
	initAPITestDB(t, secondPath)
	t.Cleanup(func() { closeAPITestDB(t) })
	var count int64
	if err := dbsqlite.DB().Model(&model.AuditEvent{}).Where("event = ?", "handoff_pending").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("audit event crossed API test database handoff: count=%d", count)
	}
}
