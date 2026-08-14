package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func securityCSRFPostRoutes() []string {
	routes := []string{
		"/api/changePass",
		"/api/addAdmin",
		"/api/deleteAdmin",
		"/api/save",
		"/api/restartApp",
		"/api/restartSb",
		"/api/linkConvert",
		"/api/subConvert",
		"/api/getCertPing",
		"/api/importdb",
		"/api/addToken",
		"/api/deleteToken",
		"/api/setTokenEnabled",
		"/api/logoutAllAdmins",
		"/api/checkOutbounds",
		"/api/rotateSubSecret",
		"/api/resetTraffic",
		"/api/ip-monitor/alice/clear",
	}
	return append(routes, securityCSRFOptionalPostRoutes()...)
}

func newSecurityCSRFTestRouter(t *testing.T, settingService *service.SettingService) *gin.Engine {
	t.Helper()
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	webPathPayload, err := json.Marshal(map[string]string{"webPath": "/"})
	if err != nil {
		t.Fatal(err)
	}
	if err := settingService.Save(dbsqlite.DB(), webPathPayload); err != nil {
		t.Fatal(err)
	}
	prepareComponentRouteMetadata(t)
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
	router.POST("/test/expire-csrf", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set(csrfExpiresKey, time.Now().Add(-time.Minute).Unix())
		if err := session.Save(); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})
	handler := &APIHandler{}
	handler.initRouter(router.Group("/api"))
	router.POST("/test/csrf-protected", handler.csrfMiddleware, func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	return router
}

func TestSecurityCSRFFetchIsStableAcrossConcurrentSessionConsumers(t *testing.T) {
	settingService := initSessionTestDB(t)
	router := newSecurityCSRFTestRouter(t, settingService)
	login := performCSRFRequest(router, http.MethodGet, "/login", "")
	if login.Code != http.StatusNoContent {
		t.Fatalf("login returned %d", login.Code)
	}

	cookies := login.Result().Cookies()
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			<-start
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/csrf", nil)
			for _, cookieValue := range cookies {
				request.AddCookie(cookieValue)
			}
			router.ServeHTTP(recorder, request)
			results <- recorder
		}()
	}
	close(start)
	first := <-results
	second := <-results
	firstToken := securityCSRFTokenFromRecorder(t, first)
	secondToken := securityCSRFTokenFromRecorder(t, second)
	if secondToken != firstToken {
		t.Fatal("a concurrent CSRF fetch invalidated an unexpired session token")
	}
	responseCookies := appendUpdatedCSRFCookies(cookies, second.Result().Cookies())
	response := performCSRFRequest(router, http.MethodPost, "/test/csrf-protected", firstToken, responseCookies...)
	if response.Code != http.StatusNoContent {
		t.Fatalf("first token failed after concurrent fetch: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSecurityCSRFMatrixRejectsMissingExpiredAndRotatedTokens(t *testing.T) {
	settingService := initSessionTestDB(t)
	router := newSecurityCSRFTestRouter(t, settingService)
	login := performCSRFRequest(router, http.MethodGet, "/login", "")
	if login.Code != http.StatusNoContent {
		t.Fatalf("login returned %d", login.Code)
	}
	cookies := login.Result().Cookies()

	for _, path := range securityCSRFPostRoutes() {
		t.Run("missing "+path, func(t *testing.T) {
			recorder := performCSRFRequest(router, http.MethodPost, path, "", cookies...)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("missing csrf for %s returned %d body=%s", path, recorder.Code, recorder.Body.String())
			}
		})
	}

	for _, path := range securityCSRFPostRoutes() {
		t.Run("expired "+path, func(t *testing.T) {
			token, freshCookies := issueSecurityCSRFToken(t, router, cookies)
			expire := performCSRFRequest(router, http.MethodPost, "/test/expire-csrf", "", freshCookies...)
			if expire.Code != http.StatusNoContent {
				t.Fatalf("expire csrf helper returned %d", expire.Code)
			}
			freshCookies = appendUpdatedCSRFCookies(freshCookies, expire.Result().Cookies())
			recorder := performCSRFRequest(router, http.MethodPost, path, token, freshCookies...)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("expired csrf for %s returned %d body=%s", path, recorder.Code, recorder.Body.String())
			}
		})
	}

	token, csrfCookies := issueSecurityCSRFToken(t, router, cookies)
	if _, err := settingService.RotateSessionGeneration(); err != nil {
		t.Fatal(err)
	}
	for _, path := range securityCSRFPostRoutes() {
		t.Run("rotated "+path, func(t *testing.T) {
			recorder := performCSRFRequest(router, http.MethodPost, path, token, csrfCookies...)
			if recorder.Code == http.StatusOK {
				t.Fatalf("rotated session csrf for %s unexpectedly reached handler: body=%s", path, recorder.Body.String())
			}
		})
	}
}

func TestSecurityCSRFMatrixDocumentsExceptions(t *testing.T) {
	settingService := initSessionTestDB(t)
	router := newSecurityCSRFTestRouter(t, settingService)

	login := performCSRFRequest(router, http.MethodPost, "/api/login", "")
	if login.Code != http.StatusForbidden {
		t.Fatalf("login without pre-auth CSRF token must be forbidden, got %d", login.Code)
	}
	logout := performCSRFRequest(router, http.MethodPost, "/api/logout", "")
	if logout.Code != http.StatusForbidden {
		t.Fatalf("POST logout without CSRF token must be forbidden, got %d", logout.Code)
	}
	sessionLogin := performCSRFRequest(router, http.MethodGet, "/login", "")
	if sessionLogin.Code != http.StatusNoContent {
		t.Fatalf("session login returned %d", sessionLogin.Code)
	}
	csrf := performCSRFRequest(router, http.MethodGet, "/api/csrf", "", sessionLogin.Result().Cookies()...)
	if csrf.Code != http.StatusOK {
		t.Fatalf("csrf endpoint with session returned %d", csrf.Code)
	}
}

func issueSecurityCSRFToken(t *testing.T, router *gin.Engine, cookies []*http.Cookie) (string, []*http.Cookie) {
	t.Helper()
	recorder := performCSRFRequest(router, http.MethodGet, "/api/csrf", "", cookies...)
	if recorder.Code != http.StatusOK {
		t.Fatalf("csrf endpoint returned %d body=%s", recorder.Code, recorder.Body.String())
	}
	return securityCSRFTokenFromRecorder(t, recorder), appendUpdatedCSRFCookies(cookies, recorder.Result().Cookies())
}

func securityCSRFTokenFromRecorder(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("csrf endpoint returned %d body=%s", recorder.Code, recorder.Body.String())
	}
	var msg Msg
	if err := json.Unmarshal(recorder.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	obj, ok := msg.Obj.(map[string]any)
	if !ok {
		t.Fatalf("unexpected csrf payload: %#v", msg.Obj)
	}
	token, ok := obj["token"].(string)
	if !ok || strings.TrimSpace(token) == "" {
		t.Fatalf("missing csrf token: %#v", obj)
	}
	return token
}
