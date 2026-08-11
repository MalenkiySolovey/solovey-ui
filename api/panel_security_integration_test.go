package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	passwordutil "github.com/MalenkiySolovey/solovey-ui/util/password"
	totputil "github.com/MalenkiySolovey/solovey-ui/util/totp"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func TestPanelSecurityForcedPasswordTransitionIsRestrictedStrictAndAtomic(t *testing.T) {
	resetRateLimitState()
	settingService := initSessionTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "webPath").Update("value", "/").Error; err != nil {
		t.Fatal(err)
	}

	const oldPassword = "temporary admin secret 2026"
	if err := (&service.UserService{}).UpdateFirstUser("admin", oldPassword); err != nil {
		t.Fatal(err)
	}
	if _, err := (&service.UserService{}).AddUser(
		"admin",
		oldPassword,
		"existing-admin",
		"Existing administrator secret 2026!",
	); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.User{}).Where("username = ?", "admin").
		Update("force_password_reset", true).Error; err != nil {
		t.Fatal(err)
	}
	var databaseList []struct {
		File string `gorm:"column:file"`
	}
	if err := dbsqlite.DB().Raw("PRAGMA database_list").Scan(&databaseList).Error; err != nil {
		t.Fatal(err)
	}
	if len(databaseList) == 0 || databaseList[0].File == "" {
		t.Fatal("active SQLite path is unavailable")
	}
	initialCredentialPath := filepath.Join(filepath.Dir(databaseList[0].File), "initial-admin.txt")
	if _, err := os.Stat(initialCredentialPath); err != nil {
		t.Fatalf("initial credential file must exist before transition: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("s-ui", cookie.NewStore([]byte("test-secret"))))
	NewAPIHandler(router.Group("/api"), nil)
	jar := integrationCookieJar{}

	preauth := performIntegrationRequest(router, httptest.NewRequest(http.MethodGet, "/api/csrf", nil), &jar)
	preauthToken := integrationCSRFToken(t, preauth)
	loginForm := url.Values{"user": {"admin"}, "pass": {oldPassword}}
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(loginForm.Encode()))
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRequest.Header.Set("Origin", "http://example.com")
	loginRequest.Header.Set(csrfHeader, preauthToken)
	login := performIntegrationRequest(router, loginRequest, &jar)
	assertIntegrationState(t, login, service.AuthStatePasswordReset)
	loginCookies := cloneCookies(jar.cookies)

	restricted := httptest.NewRequest(http.MethodGet, "/api/load", nil)
	restricted.Header.Set("X-Requested-With", "XMLHttpRequest")
	restrictedResult := performIntegrationRequest(router, restricted, &jar)
	if restrictedResult.Code != http.StatusForbidden {
		t.Fatalf("restricted session reached ordinary API: status=%d body=%s", restrictedResult.Code, restrictedResult.Body.String())
	}

	csrfResult := performIntegrationRequest(router, httptest.NewRequest(http.MethodGet, "/api/csrf", nil), &jar)
	csrfToken := integrationCSRFToken(t, csrfResult)
	wrongType := httptest.NewRequest(http.MethodPost, "/api/v1/security/password/transition", strings.NewReader("{}"))
	wrongType.Header.Set("Content-Type", "text/plain")
	wrongType.Header.Set(csrfHeader, csrfToken)
	wrongTypeResult := performIntegrationRequest(router, wrongType, &jar)
	if wrongTypeResult.Code != http.StatusBadRequest {
		t.Fatalf("non-JSON transition status=%d body=%s", wrongTypeResult.Code, wrongTypeResult.Body.String())
	}

	unknownPayload := []byte(`{"currentPassword":"temporary admin secret 2026","newUsername":"security-admin","newPassword":"Панельный секрет 2026 · v9! delta","unexpected":true}`)
	unknown := httptest.NewRequest(http.MethodPost, "/api/v1/security/password/transition", bytes.NewReader(unknownPayload))
	unknown.Header.Set("Content-Type", "application/json")
	unknown.Header.Set(csrfHeader, csrfToken)
	unknownResult := performIntegrationRequest(router, unknown, &jar)
	if unknownResult.Code != http.StatusBadRequest {
		t.Fatalf("unknown JSON field status=%d body=%s", unknownResult.Code, unknownResult.Body.String())
	}

	for _, interrupted := range []passwordTransitionRequest{
		{
			CurrentPassword: "wrong current password",
			NewUsername:     "security-admin",
			NewPassword:     "Panel replacement secret 2026!",
		},
		{
			CurrentPassword: oldPassword,
			NewUsername:     "existing-admin",
			NewPassword:     "Panel replacement secret 2026!",
		},
	} {
		interruptedPayload, err := json.Marshal(interrupted)
		if err != nil {
			t.Fatal(err)
		}
		interruptedRequest := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/security/password/transition",
			bytes.NewReader(interruptedPayload),
		)
		interruptedRequest.Header.Set("Content-Type", "application/json")
		interruptedRequest.Header.Set(csrfHeader, csrfToken)
		assertAdminFlowMsgSuccess(t, performIntegrationRequest(router, interruptedRequest, &jar), false)
		if _, err := os.Stat(initialCredentialPath); err != nil {
			t.Fatalf("interrupted transition removed initial credential file: %v", err)
		}
		var preserved model.User
		if err := dbsqlite.DB().Where("username = ?", "admin").First(&preserved).Error; err != nil {
			t.Fatalf("interrupted transition changed the original username: %v", err)
		}
		valid, _, err := passwordutil.Verify(t.Context(), preserved.Password, oldPassword)
		if err != nil || !valid || !preserved.ForcePasswordReset {
			t.Fatalf("interrupted transition lost prior recovery: valid=%v force=%v err=%v", valid, preserved.ForcePasswordReset, err)
		}
	}

	payload, err := json.Marshal(passwordTransitionRequest{
		CurrentPassword: oldPassword,
		NewUsername:     "security-admin",
		NewPassword:     "Панельный секрет 2026 · v9! delta",
	})
	if err != nil {
		t.Fatal(err)
	}
	transition := httptest.NewRequest(http.MethodPost, "/api/v1/security/password/transition", bytes.NewReader(payload))
	transition.Header.Set("Content-Type", "application/json")
	transition.Header.Set(csrfHeader, csrfToken)
	transitionResult := performIntegrationRequest(router, transition, &jar)
	assertIntegrationState(t, transitionResult, service.AuthStateAuthenticated)

	var changed model.User
	if err := dbsqlite.DB().Where("username = ?", "security-admin").First(&changed).Error; err != nil {
		t.Fatal(err)
	}
	if changed.ForcePasswordReset || !passwordutil.IsCurrent(changed.Password) {
		t.Fatalf("credential transition was not committed atomically: %#v", changed)
	}
	if _, err := os.Stat(initialCredentialPath); !os.IsNotExist(err) {
		t.Fatalf("initial credential file still exists after committed transition: %v", err)
	}

	oldRequest := httptest.NewRequest(http.MethodGet, "/api/v1/security/posture", nil)
	oldRequest.Header.Set("X-Requested-With", "XMLHttpRequest")
	oldJar := integrationCookieJar{cookies: loginCookies}
	oldResult := performIntegrationRequest(router, oldRequest, &oldJar)
	if oldResult.Code == http.StatusOK && strings.Contains(oldResult.Body.String(), `"success":true`) {
		t.Fatalf("pre-transition cookie remained valid: body=%s", oldResult.Body.String())
	}
}

func TestPanelSecurityRecoveryCodeCannotBypassForcedRecoveryTransition(t *testing.T) {
	resetRateLimitState()
	settingService := initSessionTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "webPath").Update("value", "/").Error; err != nil {
		t.Fatal(err)
	}
	const currentPassword = "current account secret 2026"
	if err := (&service.UserService{}).UpdateFirstUser("admin", currentPassword); err != nil {
		t.Fatal(err)
	}
	var admin model.User
	if err := dbsqlite.DB().Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(totputil.Period)
	mfa := service.MFAService{SettingService: *settingService, Now: func() time.Time { return now }}
	enrollment, err := mfa.BeginEnrollment(admin.Id, admin.Username)
	if err != nil {
		t.Fatal(err)
	}
	rawSecret, err := totputil.DecodeSecret(enrollment.Secret)
	if err != nil {
		t.Fatal(err)
	}
	counter := uint64(now.Unix() / int64(totputil.Period/time.Second))
	recoveryCodes, err := mfa.ConfirmEnrollment(
		admin.Id,
		totputil.Code(rawSecret, counter, totputil.Digits),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := mfa.AcknowledgeRecoveryCodes(admin.Id); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("s-ui", cookie.NewStore([]byte("test-secret"))))
	NewAPIHandler(router.Group("/api"), nil)
	jar := integrationCookieJar{}

	preauth := performIntegrationRequest(router, httptest.NewRequest(http.MethodGet, "/api/csrf", nil), &jar)
	preauthToken := integrationCSRFToken(t, preauth)
	loginForm := url.Values{"user": {"admin"}, "pass": {currentPassword}}
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(loginForm.Encode()))
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRequest.Header.Set("Origin", "http://example.com")
	loginRequest.Header.Set(csrfHeader, preauthToken)
	assertIntegrationState(t, performIntegrationRequest(router, loginRequest, &jar), service.AuthStateMFAPending)

	challengeCSRF := integrationCSRFToken(
		t,
		performIntegrationRequest(router, httptest.NewRequest(http.MethodGet, "/api/csrf", nil), &jar),
	)
	challengePayload, err := json.Marshal(mfaCodeRequest{Code: recoveryCodes[0]})
	if err != nil {
		t.Fatal(err)
	}
	challengeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/security/mfa/recovery", bytes.NewReader(challengePayload))
	challengeRequest.Header.Set("Content-Type", "application/json")
	challengeRequest.Header.Set(csrfHeader, challengeCSRF)
	challengeResult := performIntegrationRequest(router, challengeRequest, &jar)
	assertIntegrationState(t, challengeResult, service.AuthStateMFARecovery)

	restricted := httptest.NewRequest(http.MethodGet, "/api/load", nil)
	restricted.Header.Set("X-Requested-With", "XMLHttpRequest")
	restrictedResult := performIntegrationRequest(router, restricted, &jar)
	if restrictedResult.Code != http.StatusForbidden {
		t.Fatalf("recovery-only session reached ordinary API: status=%d body=%s", restrictedResult.Code, restrictedResult.Body.String())
	}

	recoveryCSRF := integrationCSRFToken(
		t,
		performIntegrationRequest(router, httptest.NewRequest(http.MethodGet, "/api/csrf", nil), &jar),
	)
	completePayload, err := json.Marshal(completeRecoveryTransitionRequest{
		NewUsername: "recovered-admin",
		NewPassword: "Recovered account secret 2026 · delta",
	})
	if err != nil {
		t.Fatal(err)
	}
	completeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/security/mfa/recovery/complete", bytes.NewReader(completePayload))
	completeRequest.Header.Set("Content-Type", "application/json")
	completeRequest.Header.Set(csrfHeader, recoveryCSRF)
	assertIntegrationState(t, performIntegrationRequest(router, completeRequest, &jar), service.AuthStateAuthenticated)

	var factorCount int64
	if err := dbsqlite.DB().Model(&model.AdminMFAFactor{}).Where("user_id = ?", admin.Id).Count(&factorCount).Error; err != nil {
		t.Fatal(err)
	}
	if factorCount != 0 {
		t.Fatalf("lost MFA factor survived recovery transition: %d", factorCount)
	}
	var recovered model.User
	if err := dbsqlite.DB().Where("username = ?", "recovered-admin").First(&recovered).Error; err != nil {
		t.Fatal(err)
	}
	valid, _, err := passwordutil.Verify(t.Context(), recovered.Password, "Recovered account secret 2026 · delta")
	if err != nil || !valid {
		t.Fatalf("recovered password invalid: valid=%v err=%v", valid, err)
	}
}

func TestPanelSecurityPasswordChangeConsumesStepUpAndRotatesGlobalSessions(t *testing.T) {
	resetRateLimitState()
	settingService := initSessionTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "webPath").Update("value", "/").Error; err != nil {
		t.Fatal(err)
	}
	const oldPassword = "Current account password 2026!"
	const newPassword = "Changed account password 2026!"
	if err := (&service.UserService{}).UpdateFirstUser("admin", oldPassword); err != nil {
		t.Fatal(err)
	}

	router, _ := newAdminFlowRouter(t)
	jar := &integrationCookieJar{}
	loginAdminFlowUser(t, router, jar, "admin", oldPassword)
	oldCookies := cloneCookies(jar.cookies)
	var before model.User
	if err := dbsqlite.DB().Where("username = ?", "admin").First(&before).Error; err != nil {
		t.Fatal(err)
	}

	grant, csrf := adminFlowStepUp(
		t,
		router,
		jar,
		oldPassword,
		"admin.credential",
		"user:"+strconv.FormatUint(uint64(before.Id), 10),
	)
	payload, err := json.Marshal(passwordChangeRequest{
		NewUsername: "secure-admin",
		NewPassword: newPassword,
		StepUpToken: grant,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/security/password/change", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeader, csrf)
	result := performIntegrationRequest(router, request, jar)
	assertAdminFlowMsgSuccess(t, result, true)

	var changed model.User
	if err := dbsqlite.DB().Where("username = ?", "secure-admin").First(&changed).Error; err != nil {
		t.Fatal(err)
	}
	valid, _, err := passwordutil.Verify(t.Context(), changed.Password, newPassword)
	if err != nil || !valid || changed.CredentialGeneration < 2 {
		t.Fatalf("new credential not committed: valid=%v generation=%d err=%v", valid, changed.CredentialGeneration, err)
	}
	oldValid, _, err := passwordutil.Verify(t.Context(), changed.Password, oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	if oldValid {
		t.Fatal("old password remained valid")
	}

	oldJar := &integrationCookieJar{cookies: oldCookies}
	oldRequest := httptest.NewRequest(http.MethodGet, "/api/v1/security/posture", nil)
	oldRequest.Header.Set("X-Requested-With", "XMLHttpRequest")
	oldResult := performIntegrationRequest(router, oldRequest, oldJar)
	if oldResult.Code == http.StatusOK && strings.Contains(oldResult.Body.String(), `"success":true`) {
		t.Fatalf("pre-change cookie remained valid: %s", oldResult.Body.String())
	}

	replayCSRF := adminFlowCSRFToken(t, router, jar)
	replay := httptest.NewRequest(http.MethodPost, "/api/v1/security/password/change", bytes.NewReader(payload))
	replay.Header.Set("Content-Type", "application/json")
	replay.Header.Set(csrfHeader, replayCSRF)
	if replayResult := performIntegrationRequest(router, replay, jar); replayResult.Code != http.StatusOK ||
		assertAdminFlowMsgSuccess(t, replayResult, false).Msg == "" {
		t.Fatalf("consumed grant replay was not rejected: status=%d body=%s", replayResult.Code, replayResult.Body.String())
	}
}

func TestPanelSecurityLegacySessionPolicyRequiresStepUpAdoption(t *testing.T) {
	resetRateLimitState()
	settingService := initSessionTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "webPath").Update("value", "/").Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "sessionLifetimePolicy").Update("value", service.LifetimePostureLegacyUnbounded).Error; err != nil {
		t.Fatal(err)
	}
	const password = "Current administrator session policy 2026!"
	if err := (&service.UserService{}).UpdateFirstUser("admin", password); err != nil {
		t.Fatal(err)
	}

	router, _ := newAdminFlowRouter(t)
	jar := &integrationCookieJar{}
	loginAdminFlowUser(t, router, jar, "admin", password)
	grant, csrf := adminFlowStepUp(t, router, jar, password, "sessions.adopt_bounded", "policy:bounded_v1")
	payload, err := json.Marshal(stepUpTokenRequest{StepUpToken: grant})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/security/sessions/adopt-bounded", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeader, csrf)
	result := performIntegrationRequest(router, request, jar)
	assertAdminFlowMsgSuccess(t, result, true)

	resolved, err := settingService.ResolveSessionLifetime()
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Posture != service.LifetimePostureBoundedV1 {
		t.Fatalf("adopted session posture=%#v", resolved)
	}
	postureRequest := httptest.NewRequest(http.MethodGet, "/api/v1/security/posture", nil)
	postureRequest.Header.Set("X-Requested-With", "XMLHttpRequest")
	postureResult := performIntegrationRequest(router, postureRequest, jar)
	if postureResult.Code != http.StatusOK || !strings.Contains(postureResult.Body.String(), `"sessionLifetimePolicy":"bounded_v1"`) {
		t.Fatalf("post-adoption posture status=%d body=%s", postureResult.Code, postureResult.Body.String())
	}
}

func TestPanelSecurityLegacyHighRiskRoutesRequireBoundSingleUseStepUp(t *testing.T) {
	resetRateLimitState()
	settingService := initSessionTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "webPath").Update("value", "/").Error; err != nil {
		t.Fatal(err)
	}
	const password = "Current administrator secret 2026!"
	if err := (&service.UserService{}).UpdateFirstUser("admin", password); err != nil {
		t.Fatal(err)
	}
	router, _ := newAdminFlowRouter(t)
	jar := &integrationCookieJar{}
	loginAdminFlowUser(t, router, jar, "admin", password)
	csrf := adminFlowCSRFToken(t, router, jar)

	missing := adminFlowPost(t, router, jar, csrf, "", "/api/addToken", url.Values{
		"desc":   {"missing grant"},
		"expiry": {"0"},
		"scope":  {"admin"},
	})
	if missing.Code != http.StatusForbidden {
		t.Fatalf("token creation without step-up status=%d body=%s", missing.Code, missing.Body.String())
	}

	grant, csrf := adminFlowStepUp(t, router, jar, password, "token.create", "new-token")
	created := adminFlowPost(t, router, jar, csrf, grant, "/api/addToken", url.Values{
		"desc":   {"protected token"},
		"expiry": {"0"},
		"scope":  {"admin"},
	})
	assertAdminFlowMsgSuccess(t, created, true)

	replay := adminFlowPost(t, router, jar, csrf, grant, "/api/addToken", url.Values{
		"desc":   {"replay token"},
		"expiry": {"0"},
		"scope":  {"admin"},
	})
	if replay.Code != http.StatusForbidden {
		t.Fatalf("step-up replay status=%d body=%s", replay.Code, replay.Body.String())
	}

	identityGrant, identityCSRF := adminFlowStepUp(t, router, jar, password, "token.create", "new-token")
	identityForm := url.Values{
		"desc":   {"client identity bound token"},
		"expiry": {"0"},
		"scope":  {"admin"},
	}
	changedIdentity := httptest.NewRequest(http.MethodPost, "/api/addToken", strings.NewReader(identityForm.Encode()))
	changedIdentity.RemoteAddr = "192.0.2.44:1234"
	changedIdentity.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	changedIdentity.Header.Set("X-Requested-With", "XMLHttpRequest")
	changedIdentity.Header.Set(csrfHeader, identityCSRF)
	changedIdentity.Header.Set(stepUpHeader, identityGrant)
	changedIdentityResult := performIntegrationRequest(router, changedIdentity, jar)
	if changedIdentityResult.Code != http.StatusForbidden {
		t.Fatalf("step-up crossed client identity: status=%d body=%s", changedIdentityResult.Code, changedIdentityResult.Body.String())
	}
	// A mismatched identity must not consume the grant. The exact issuing
	// identity can still perform its intended one-time action.
	identityCSRF = adminFlowCSRFToken(t, router, jar)
	boundIdentityResult := adminFlowPost(t, router, jar, identityCSRF, identityGrant, "/api/addToken", identityForm)
	assertAdminFlowMsgSuccess(t, boundIdentityResult, true)

	proxyRevisionGrant, proxyRevisionCSRF := adminFlowStepUp(t, router, jar, password, "token.create", "new-token")
	t.Setenv("SUI_TRUSTED_PROXIES", "192.0.2.1/32")
	changedProxyRevision := httptest.NewRequest(http.MethodPost, "/api/addToken", strings.NewReader(identityForm.Encode()))
	changedProxyRevision.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	changedProxyRevision.Header.Set("X-Requested-With", "XMLHttpRequest")
	changedProxyRevision.Header.Set("X-Forwarded-For", "198.51.100.7")
	changedProxyRevision.Header.Set("X-Forwarded-Proto", "http")
	changedProxyRevision.Header.Set(csrfHeader, proxyRevisionCSRF)
	changedProxyRevision.Header.Set(stepUpHeader, proxyRevisionGrant)
	changedProxyResult := performIntegrationRequest(router, changedProxyRevision, jar)
	if changedProxyResult.Code != http.StatusForbidden {
		t.Fatalf("step-up crossed trusted-proxy revision: status=%d body=%s", changedProxyResult.Code, changedProxyResult.Body.String())
	}
	t.Setenv("SUI_TRUSTED_PROXIES", "")
	proxyRevisionCSRF = adminFlowCSRFToken(t, router, jar)
	boundProxyRevisionResult := adminFlowPost(t, router, jar, proxyRevisionCSRF, proxyRevisionGrant, "/api/addToken", identityForm)
	assertAdminFlowMsgSuccess(t, boundProxyRevisionResult, true)

	restore := adminFlowPost(t, router, jar, csrf, "", "/api/importdb", url.Values{})
	if restore.Code != http.StatusForbidden {
		t.Fatalf("restore without step-up status=%d body=%s", restore.Code, restore.Body.String())
	}
}

func assertIntegrationState(t *testing.T, recorder *httptest.ResponseRecorder, expected string) {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("request returned %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Success bool `json:"success"`
		Obj     struct {
			State string `json:"state"`
		} `json:"obj"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.Obj.State != expected {
		t.Fatalf("state=%q success=%v, want %q body=%s", response.Obj.State, response.Success, expected, recorder.Body.String())
	}
}

func cloneCookies(cookies []*http.Cookie) []*http.Cookie {
	result := make([]*http.Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		clone := *cookie
		result = append(result, &clone)
	}
	return result
}
