package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

// Scope and legacy-header tests start after the bootstrap credential reset.
// Tests of the reset boundary explicitly set the policy back to true.
func completeTokenOwnerResetForTest(t *testing.T) {
	t.Helper()
	if err := dbsqlite.DB().Model(&model.User{}).Where("id = ?", 1).Update("force_password_reset", false).Error; err != nil {
		t.Fatal(err)
	}
}

func TestAPIV2CachedTokenUsesCurrentAccountAndPermission(t *testing.T) {
	initSessionTestDB(t)
	completeTokenOwnerResetForTest(t)
	s := &service.UserService{}
	plain, err := s.AddToken("admin", 0, "current account", "admin")
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := NewAPIv2Handler(router.Group("/apiv2"))
	router.GET("/principal", h.checkToken, func(c *gin.Context) {
		c.String(http.StatusOK, c.GetString(apiUsernameKey)+":"+c.GetString(apiTokenScopeKey))
	})
	request := func(wantStatus int, wantPrincipal string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/principal", nil)
		req.Header.Set("Authorization", "Bearer "+plain)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != wantStatus {
			t.Fatalf("authorization status=%d, want %d", res.Code, wantStatus)
		}
		if wantStatus == http.StatusOK && res.Body.String() != wantPrincipal {
			t.Fatal("authorization returned stale identity or permission")
		}
	}
	request(http.StatusOK, "admin:admin")
	db := dbsqlite.DB()
	if err := db.Model(&model.User{}).Where("id = ?", 1).Update("force_password_reset", true).Error; err != nil {
		t.Fatal(err)
	}
	request(http.StatusUnauthorized, "")
	if err := db.Model(&model.User{}).Where("id = ?", 1).Updates(map[string]any{"force_password_reset": false, "username": "current-principal"}).Error; err != nil {
		t.Fatal(err)
	}
	request(http.StatusOK, "current-principal:admin")
	if err := db.Model(&model.Tokens{}).Where("user_id = ?", 1).Update("scope", "read").Error; err != nil {
		t.Fatal(err)
	}
	request(http.StatusOK, "current-principal:read")
	if err := db.Model(&model.Tokens{}).Where("user_id = ?", 1).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}
	request(http.StatusUnauthorized, "")
	if err := db.Where("user_id = ?", 1).Delete(&model.Tokens{}).Error; err != nil {
		t.Fatal(err)
	}
	request(http.StatusUnauthorized, "")
}

func TestAPIV2OverlappingReloadCannotRepublishOldAuthorization(t *testing.T) {
	for _, latestFails := range []bool{false, true} {
		t.Run(strconv.FormatBool(latestFails), func(t *testing.T) {
			started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			h := &APIv2Handler{tokens: map[string]TokenInMemory{"earlier": {ID: 1}}}
			h.loadTokens = func() ([]byte, error) {
				if calls.Add(1) == 1 {
					close(started)
					<-release
					return json.Marshal([]TokenInMemory{{ID: 1, TokenHash: "earlier"}})
				}
				if latestFails {
					return nil, errors.New("fixture load failed")
				}
				return json.Marshal([]TokenInMemory{{ID: 2, TokenHash: "current"}})
			}
			go func() { h.ReloadTokens(); close(done) }()
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				close(release)
				t.Fatal("reload did not reach barrier")
			}
			h.tokensMu.RLock()
			during := len(h.tokens)
			h.tokensMu.RUnlock()
			h.ReloadTokens()
			close(release)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("older reload did not finish")
			}
			if during != 0 {
				t.Fatal("old snapshot remained authorized during reload")
			}
			h.tokensMu.RLock()
			defer h.tokensMu.RUnlock()
			if _, stale := h.tokens["earlier"]; stale {
				t.Fatal("older loader republished an invalidated snapshot")
			}
			if _, current := h.tokens["current"]; current == latestFails {
				t.Fatal("snapshot does not reflect the latest reload outcome")
			}
		})
	}
}

func TestTokenHTTPMutationsBindOwnerAndReloadOnlyAfterSuccess(t *testing.T) {
	resetRateLimitState()
	settings := initSessionTestDB(t)
	if _, err := settings.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "webPath").Update("value", "/").Error; err != nil {
		t.Fatal(err)
	}
	s := &service.UserService{}
	if err := s.UpdateFirstUser("admin", "current-password"); err != nil {
		t.Fatal(err)
	}
	other, err := s.AddUser("admin", "current-password", "other-owner", "other-owner-password")
	if err != nil {
		t.Fatal(err)
	}
	own := model.Tokens{UserId: 1, TokenHash: "own-http-fixture-digest", Scope: "read"}
	foreign := model.Tokens{UserId: other.Id, TokenHash: "foreign-http-fixture-digest", Scope: "read"}
	for _, token := range []*model.Tokens{&own, &foreign} {
		if err := dbsqlite.DB().Create(token).Error; err != nil {
			t.Fatal(err)
		}
	}
	router, h := newAdminFlowRouter(t)
	var reloads int
	loader := h.loadTokens
	h.loadTokens = func() ([]byte, error) { reloads++; return loader() }
	jar := &integrationCookieJar{}
	loginAdminFlowUser(t, router, jar, "admin", "current-password")
	for _, action := range []string{"deleteToken", "setTokenEnabled"} {
		operation := "token.revoke"
		if action == "setTokenEnabled" {
			operation = "token.change"
		}
		var deniedMessage string
		for _, id := range []string{strconv.FormatUint(uint64(foreign.Id), 10), "999999999"} {
			grant, csrf := adminFlowStepUp(t, router, jar, "current-password", operation, "token:"+id)
			res := adminFlowPost(t, router, jar, csrf, grant, "/api/"+action, url.Values{"id": {id}, "enabled": {"false"}})
			msg := assertAdminFlowMsgSuccess(t, res, false)
			if deniedMessage != "" && msg.Msg != deniedMessage {
				t.Fatal("foreign and missing token responses differ")
			}
			deniedMessage = msg.Msg
		}
	}
	if reloads != 0 {
		t.Fatal("failed mutations reloaded token authorization")
	}
	var retained model.Tokens
	if err := dbsqlite.DB().First(&retained, foreign.Id).Error; err != nil || !retained.Enabled {
		t.Fatalf("foreign token changed: err=%v", err)
	}
	// Use a fresh authenticated session for the success probes, keeping each
	// fixture session within the production password-verification rate budget.
	jar = &integrationCookieJar{}
	loginAdminFlowUser(t, router, jar, "admin", "current-password")
	for _, action := range []string{"setTokenEnabled", "deleteToken"} {
		operation := "token.change"
		if action == "deleteToken" {
			operation = "token.revoke"
		}
		id := strconv.FormatUint(uint64(own.Id), 10)
		grant, csrf := adminFlowStepUp(t, router, jar, "current-password", operation, "token:"+id)
		res := adminFlowPost(t, router, jar, csrf, grant, "/api/"+action, url.Values{"id": {id}, "enabled": {"false"}})
		assertAdminFlowMsgSuccess(t, res, true)
	}
	if reloads != 2 {
		t.Fatalf("successful mutation reload count=%d, want 2", reloads)
	}
}

func TestCredentialHTTPMutationReloadsTokenIdentityOnlyAfterCommit(t *testing.T) {
	resetRateLimitState()
	settings := initSessionTestDB(t)
	if _, err := settings.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "webPath").Update("value", "/").Error; err != nil {
		t.Fatal(err)
	}
	s := &service.UserService{}
	if err := s.UpdateFirstUser("admin", "current-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddToken("admin", 0, "credential flow", "read"); err != nil {
		t.Fatal(err)
	}
	router, h := newAdminFlowRouter(t)
	var reloads int
	loader := h.loadTokens
	h.loadTokens = func() ([]byte, error) { reloads++; return loader() }
	jar := &integrationCookieJar{}
	loginAdminFlowUser(t, router, jar, "admin", "current-password")
	for _, valid := range []bool{false, true} {
		grant, csrf := adminFlowStepUp(t, router, jar, "current-password", "admin.credential", "user:1")
		newUsername := "renamed-account"
		if !valid {
			newUsername = ""
		}
		res := adminFlowPost(t, router, jar, csrf, grant, "/api/changePass", url.Values{
			"oldPass": {"current-password"}, "newUsername": {newUsername}, "newPass": {"replacement-password"},
		})
		assertAdminFlowMsgSuccess(t, res, valid)
		if !valid && reloads != 0 {
			t.Fatal("failed credentials change reloaded authorization")
		}
	}
	if reloads != 1 {
		t.Fatalf("credential commit reload count=%d, want 1", reloads)
	}
	h.tokensMu.RLock()
	defer h.tokensMu.RUnlock()
	for _, token := range h.tokens {
		if token.Username != "renamed-account" {
			t.Fatal("credential commit retained cached old principal")
		}
	}
}
