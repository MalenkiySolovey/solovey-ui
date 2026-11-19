//go:build !minimal

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-backend/http/securityroutes"
	totputil "github.com/MalenkiySolovey/solovey-backend/security/totp"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-backend/storage/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/supervisor"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service"
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
	installedPath := prepareRemovalOwner(t, "telegram")
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
	if metadata, exists, err := installstate.Load(installedPath); err != nil || !exists || len(metadata.Components) != 0 {
		t.Fatalf("removal did not commit installed metadata: %+v %v %v", metadata, exists, err)
	}
	if replay := adminFlowPost(t, router, jar, csrf, grant, path, nil); replay.Code != http.StatusForbidden {
		t.Fatal("removal grant replay succeeded")
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Set(apiUsernameKey, "admin")
	c.Set(apiTokenScopeKey, "admin")
	if (&APIHandler{ApiService: newComposedApiService()}).requireStepUpAction(c, "component.remove", "component:telegram") || recorder.Code != http.StatusForbidden {
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
	prepareRemovalOwner(t, "telegram")
	router, _ := newAdminFlowRouter(t)
	jar := &integrationCookieJar{}
	loginAdminFlowUser(t, router, jar, "admin", password)
	csrf := adminFlowCSRFToken(t, router, jar)
	payload, _ := json.Marshal(securityroutes.MFACodeRequest{Code: code(time.Now())})
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

func prepareRemovalOwner(t *testing.T, id string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, path)
	t.Setenv(installstate.ManagementEnv, "runtime")
	if err := installstate.Store(path, installstate.Metadata{Version: 1, Profile: "full", Binary: "full",
		Components: []installstate.InstalledComponent{{ID: id, Delivery: manifest.DeliveryInProcess, Installed: true}}}); err != nil {
		t.Fatal(err)
	}
	key := enabledstate.SettingKey(id)
	if err := dbsqlite.DB().Where("key = ?", key).Assign(model.Setting{Value: "false"}).FirstOrCreate(&model.Setting{Key: key}).Error; err != nil {
		t.Fatal(err)
	}
	owner := supervisor.New(componenthost.Deps{})
	service.RegisterComponentRemovalAdmitter(owner.TryAdmitRemoval)
	service.RegisterComponentSettingsReconciler(owner.Reconcile)
	t.Cleanup(func() {
		service.RegisterComponentSettingsReconciler(nil)
		service.RegisterComponentRemovalAdmitter(nil)
	})
	return path
}
