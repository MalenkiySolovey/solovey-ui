//go:build !minimal

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	totputil "github.com/MalenkiySolovey/solovey-ui/util/totp"
	"github.com/gin-gonic/gin"
)

func TestComponentRemovalRequiresBoundSingleUseGrant(t *testing.T) {
	resetRateLimitState()
	settings := initSessionTestDB(t)
	if _, err := settings.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "webPath").Update("value", "/").Error; err != nil {
		t.Fatal(err)
	}
	const password = "Component removal administrator 2026!"
	if err := (&service.UserService{}).UpdateFirstUser("admin", password); err != nil {
		t.Fatal(err)
	}
	prepareComponentRouteMetadata(t)
	router, _ := newAdminFlowRouter(t)
	jar := &integrationCookieJar{}
	loginAdminFlowUser(t, router, jar, "admin", password)
	csrf := adminFlowCSRFToken(t, router, jar)
	const path = "/api/update/components/telegram/remove"
	missing := adminFlowPost(t, router, jar, csrf, "", path, url.Values{"password": {password}})
	if missing.Code != http.StatusForbidden {
		t.Fatalf("password-only removal: %d %s", missing.Code, missing.Body.String())
	}
	for _, binding := range []struct{ operation, target string }{
		{"data.drop", "component:telegram"}, {"component.remove", "component:fallback-html"},
	} {
		grant, csrf := adminFlowStepUp(t, router, jar, password, binding.operation, binding.target)
		denied := adminFlowPost(t, router, jar, csrf, grant, path, nil)
		if denied.Code != http.StatusForbidden {
			t.Fatalf("wrong action/target admitted: %d %s", denied.Code, denied.Body.String())
		}
	}
	grant, csrf := adminFlowStepUp(t, router, jar, password, "component.remove", "component:telegram")
	assertAdminFlowMsgSuccess(t, adminFlowPost(t, router, jar, csrf, grant, path, nil), true)
	if replay := adminFlowPost(t, router, jar, csrf, grant, path, nil); replay.Code != http.StatusForbidden {
		t.Fatal("removal grant replay succeeded")
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Set(apiUsernameKey, "admin")
	c.Set(apiTokenScopeKey, "admin")
	if (&APIHandler{}).requireStepUpAction(c, "component.remove", "component:telegram") || recorder.Code != http.StatusForbidden {
		t.Fatal("bearer token crossed the browser assurance boundary")
	}
}

func TestComponentRemovalMFARequiresCurrentAssurance(t *testing.T) {
	resetRateLimitState()
	settings := initSessionTestDB(t)
	if _, err := settings.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "webPath").Update("value", "/").Error; err != nil {
		t.Fatal(err)
	}
	const password = "MFA component administrator 2026!"
	if err := (&service.UserService{}).UpdateFirstUser("admin", password); err != nil {
		t.Fatal(err)
	}
	var admin model.User
	if err := dbsqlite.DB().Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(totputil.Period)
	previous := now.Add(-totputil.Period)
	mfa := service.MFAService{SettingService: *settings, Now: func() time.Time { return previous }}
	enrollment, err := mfa.BeginEnrollment(admin.Id, admin.Username)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := totputil.DecodeSecret(enrollment.Secret)
	if err != nil {
		t.Fatal(err)
	}
	code := func(at time.Time) string {
		return totputil.Code(secret, uint64(at.Unix()/int64(totputil.Period/time.Second)), totputil.Digits)
	}
	recovery, err := mfa.ConfirmEnrollment(admin.Id, code(previous))
	if err != nil {
		t.Fatal(err)
	}
	if err := mfa.AcknowledgeRecoveryCodes(admin.Id); err != nil {
		t.Fatal(err)
	}
	prepareComponentRouteMetadata(t)
	router, _ := newAdminFlowRouter(t)
	jar := &integrationCookieJar{}
	loginAdminFlowUser(t, router, jar, "admin", password)
	csrf := adminFlowCSRFToken(t, router, jar)
	payload, _ := json.Marshal(mfaCodeRequest{Code: code(time.Now())})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/security/mfa/challenge", strings.NewReader(string(payload)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Requested-With", "XMLHttpRequest")
	request.Header.Set(csrfHeader, csrf)
	assertAdminFlowMsgSuccess(t, performIntegrationRequest(router, request, jar), true)
	issue := func(method, credential string) *httptest.ResponseRecorder {
		csrf := adminFlowCSRFToken(t, router, jar)
		payload, _ := json.Marshal(issueStepUpRequest{Method: method, Credential: credential, OperationKind: "component.remove", TargetDigest: securityTargetDigest("component:telegram")})
		request := httptest.NewRequest(http.MethodPost, "/api/v1/security/step-up", strings.NewReader(string(payload)))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Requested-With", "XMLHttpRequest")
		request.Header.Set(csrfHeader, csrf)
		return performIntegrationRequest(router, request, jar)
	}
	assertAdminFlowMsgSuccess(t, issue("password", password), false)
	msg := assertAdminFlowMsgSuccess(t, issue("recovery", recovery[0]), true)
	token := msg.Obj.(map[string]any)["token"].(string)
	csrf = adminFlowCSRFToken(t, router, jar)
	assertAdminFlowMsgSuccess(t, adminFlowPost(t, router, jar, csrf, token, "/api/update/components/telegram/remove", nil), true)
}
