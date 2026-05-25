package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/deposist/s-ui-x/database"
	"github.com/deposist/s-ui-x/database/model"
	"github.com/deposist/s-ui-x/service"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func TestSecurityLoginLockoutBlocksAuditsAndRecovers(t *testing.T) {
	resetRateLimitState()
	settingService := initSessionTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	webPathPayload, err := json.Marshal(map[string]string{"webPath": "/"})
	if err != nil {
		t.Fatal(err)
	}
	if err := settingService.Save(database.GetDB(), webPathPayload); err != nil {
		t.Fatal(err)
	}
	if err := (&service.UserService{}).UpdateFirstUser("admin", "correct-password"); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("s-ui", cookie.NewStore([]byte("test-secret"))))
	NewAPIHandler(router.Group("/api"), nil)

	const ip = "198.51.100.77"
	for i := 0; i < 10; i++ {
		recorder := performSecurityLogin(router, ip, "admin", "wrong-password")
		if recorder.Code != http.StatusOK {
			t.Fatalf("wrong login %d returned status %d", i+1, recorder.Code)
		}
	}

	var blocked model.AuditEvent
	if err := database.GetDB().Where("event = ?", "login_blocked").Order("id desc").First(&blocked).Error; err != nil {
		t.Fatal(err)
	}
	if blocked.Actor != "admin" || blocked.Resource != "auth" {
		t.Fatalf("unexpected login_blocked audit: %#v", blocked)
	}

	blockedLogin := performSecurityLogin(router, ip, "admin", "correct-password")
	assertSecurityLoginFailureContains(t, blockedLogin, "too many login attempts")

	forceLoginWindowElapsed(ip)
	recovered := performSecurityLogin(router, ip, "admin", "correct-password")
	assertSecurityLoginSuccess(t, recovered)

	for i := 0; i < loginRateLimitMax; i++ {
		_ = performSecurityLogin(router, ip, "admin", "wrong-password")
	}
	resetLoginFailures(ip)
	afterReset := performSecurityLogin(router, ip, "admin", "correct-password")
	assertSecurityLoginSuccess(t, afterReset)
}

func performSecurityLogin(router *gin.Engine, ip string, username string, password string) *httptest.ResponseRecorder {
	form := url.Values{}
	form.Set("user", username)
	form.Set("pass", password)
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(form.Encode()))
	req.RemoteAddr = ip + ":12345"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func assertSecurityLoginSuccess(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	var msg Msg
	if err := json.Unmarshal(recorder.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if !msg.Success {
		t.Fatalf("expected successful login, got %#v body=%s", msg, recorder.Body.String())
	}
	if findCookieByName(recorder.Result().Cookies(), "s-ui") == nil {
		t.Fatal("successful login did not set session cookie")
	}
}

func assertSecurityLoginFailureContains(t *testing.T, recorder *httptest.ResponseRecorder, want string) {
	t.Helper()
	var msg Msg
	if err := json.Unmarshal(recorder.Body.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Success || !strings.Contains(msg.Msg, want) {
		t.Fatalf("expected login failure containing %q, got %#v body=%s", want, msg, recorder.Body.String())
	}
}

func forceLoginWindowElapsed(key string) {
	loginRateLimitMu.Lock()
	defer loginRateLimitMu.Unlock()
	attempt := loginRateLimits[key]
	attempt.firstFailAt = time.Now().Add(-loginRateLimitWindow - time.Second)
	attempt.blockedUntil = time.Now().Add(-time.Second)
	loginRateLimits[key] = attempt
	loginRateLimitGC = time.Time{}
}
