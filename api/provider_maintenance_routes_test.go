package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	"github.com/MalenkiySolovey/solovey-ui/service"
	updateservice "github.com/MalenkiySolovey/solovey-ui/service/update"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func providerMaintenanceRequestFixture() providerMaintainRequest {
	request := updateservice.ProviderTransitionRequest{Channel: "main", TargetProvider: "shtorm-extended", ExpectedSequence: 19,
		ExpectedManifestDigest: strings.Repeat("a", 64), ExpectedArtifactSetDigest: strings.Repeat("b", 64),
		ExpectedConfigurationRevision: strings.Repeat("c", 64), ExpectedProviderRevision: strings.Repeat("d", 64),
		IdempotencyKey: "provider-maintenance-api", Acknowledged: true}
	return providerMaintainRequest{ProviderTransitionRequest: request, Confirmation: updateservice.ProviderTransitionConfirmation(request)}
}

type providerMaintenanceDeadlineWriter struct {
	gin.ResponseWriter
	writes *[]time.Time
}

func (w providerMaintenanceDeadlineWriter) SetWriteDeadline(deadline time.Time) error {
	*w.writes = append(*w.writes, deadline)
	return nil
}

func TestProviderMaintenanceTransportRequiresSessionAndOneUseStepUp(t *testing.T) {
	router, cookies := updateTestRouter(t)
	body, _ := json.Marshal(providerMaintenanceRequestFixture())
	for _, path := range []string{"/api/v1/operations/update/provider/preview", "/api/v1/operations/update/provider/maintain"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body))))
		if recorder.Code != http.StatusForbidden {
			t.Fatal("unauthenticated provider maintenance transport admitted", path, recorder.Code)
		}
	}
	recorder := updateRequest(router, cookies, http.MethodPost, "/api/v1/operations/update/provider/maintain", string(body))
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "step-up") {
		t.Fatal("owner did not require a one-use step-up grant before maintenance", recorder.Code, recorder.Body.String())
	}
}

func TestProviderMaintenanceTransportRejectsUnboundedAndCallerSuppliedAuthority(t *testing.T) {
	router, cookies := updateTestRouter(t)
	body, _ := json.Marshal(providerMaintenanceRequestFixture())
	for _, member := range []string{`"command":"sh"`, `"path":"/foreign"`, `"authorize":true`, `"sourceURL":"https://foreign"`, `"scope":"admin"`} {
		forged := strings.TrimSuffix(string(body), "}") + "," + member + "}"
		recorder := updateRequest(router, cookies, http.MethodPost, "/api/v1/operations/update/provider/maintain", forged)
		if recorder.Code != http.StatusBadRequest {
			t.Fatal("unknown provider authority reached semantic execution", member, recorder.Code)
		}
	}
	request := providerMaintenanceRequestFixture()
	request.TargetProvider = "sing-box"
	changed, _ := json.Marshal(request)
	if got := updateRequest(router, cookies, http.MethodPost, "/api/v1/operations/update/provider/maintain", string(changed)); got.Code != http.StatusBadRequest {
		t.Fatal("altered provider plan borrowed an old confirmation", got.Code)
	}
}

func TestProviderMaintenanceRealOwnerConsumesOnlyMatchingSessionCSRFAndOneUseGrant(t *testing.T) {
	resetRateLimitState()
	settings := initSessionTestDB(t)
	if _, err := settings.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "webPath").Update("value", "/").Error; err != nil {
		t.Fatal(err)
	}
	const password = "Provider maintenance authentication test 2026!"
	if err := (&service.UserService{}).UpdateFirstUser("admin", password); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	var deadlines []time.Time
	router.Use(func(c *gin.Context) {
		c.Writer = providerMaintenanceDeadlineWriter{ResponseWriter: c.Writer, writes: &deadlines}
		c.Next()
	})
	store := newAPITestSessionStore(t)
	store.Options(sessions.Options{Path: "/", HttpOnly: true})
	router.Use(sessions.Sessions("s-ui", store))
	api := &APIHandler{ApiService: NewApiService(), apiv2: NewAPIv2Handler(router.Group("/apiv2"))}
	api.Update = updateservice.NewLifecycleManager(updateservice.Repository{DB: dbsqlite.DB}, nil, nil, release.Source{}, release.TrustStore{})
	api.initRouter(router.Group("/api"))
	jar := &integrationCookieJar{}
	loginAdminFlowUser(t, router, jar, "admin", password)
	actual := providerMaintenanceRequestFixture()
	post := func(request providerMaintainRequest, csrf, grant string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(request)
		httpRequest := httptest.NewRequest(http.MethodPost, "/api/v1/operations/update/provider/maintain", strings.NewReader(string(body)))
		httpRequest.Header.Set("Content-Type", "application/json")
		httpRequest.Header.Set(csrfHeader, csrf)
		httpRequest.Header.Set(stepUpHeader, grant)
		return performIntegrationRequest(router, httpRequest, jar)
	}
	invocation := updateservice.ProviderTransitionAction(actual.ProviderTransitionRequest)
	grant, csrf := adminFlowStepUp(t, router, jar, password, invocation.ID, invocation.Target)
	if got := post(actual, "", grant); got.Code != http.StatusForbidden {
		t.Fatal("provider action bypassed CSRF")
	}
	changed := actual
	changed.TargetProvider = "sing-box"
	changed.Confirmation = updateservice.ProviderTransitionConfirmation(changed.ProviderTransitionRequest)
	if got := post(changed, csrf, grant); got.Code != http.StatusForbidden {
		t.Fatal("provider action borrowed a different plan's grant")
	}
	if len(deadlines) != 0 {
		t.Fatal("unapproved provider action gained an extended transport deadline")
	}
	// This manager deliberately has no ConfigService composition. Exact
	// authorization is consumed before that fail-closed owner admission;
	// no download, maintenance hold or real activation is performed here.
	if got := post(actual, csrf, grant); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), updateservice.ReasonCode(updateservice.ErrCoreProviderUnverified)) {
		t.Fatal("exact provider action did not reach the actual Update owner", got.Code, got.Body.String())
	}
	if len(deadlines) != 1 || time.Until(deadlines[0]) <= 4*time.Minute || time.Until(deadlines[0]) > 6*time.Minute {
		t.Fatal("approved provider action lost its bounded response deadline", deadlines)
	}
	if got := post(actual, csrf, grant); got.Code != http.StatusForbidden {
		t.Fatal("provider action replayed a consumed grant")
	}
	if len(deadlines) != 1 {
		t.Fatal("replayed provider action gained an extended transport deadline")
	}
}

func TestProviderMaintenanceActionDoesNotTreatBearerScopeAsSessionAssurance(t *testing.T) {
	initSessionTestDB(t)
	api := &APIHandler{ApiService: NewApiService()}
	for _, scope := range []string{"read", "update", "admin"} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/operations/update/provider/maintain", nil)
		ctx.Set(apiUsernameKey, "test-operator")
		ctx.Set(apiTokenScopeKey, scope)
		invocation := updateservice.ProviderTransitionAction(providerMaintenanceRequestFixture().ProviderTransitionRequest)
		if _, err := actions.Require(ctx.Request.Context(), invocation, api.actionPolicy(api.requireStepUpAction)(ctx)); err == nil || recorder.Code != http.StatusForbidden {
			t.Fatal("bearer scope replaced session assurance", scope, recorder.Code)
		}
	}
}
