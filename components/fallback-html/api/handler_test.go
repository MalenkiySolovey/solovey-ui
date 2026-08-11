//go:build !minimal

package api

import (
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/components/fallback-html/authority"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type apiResponse struct {
	Success bool            `json:"success"`
	Obj     json.RawMessage `json:"obj"`
	Msg     string          `json:"msg"`
}

func TestRoutesCreatePublishAndReportHealth(t *testing.T) {
	router, _ := newFallbackAPITestRouter(t, true)
	create := performFallbackAPIRequest(router, http.MethodPost, "/api/components/fallback-html/sites", `{"name":"Public test","enabled":true}`)
	assertFallbackAPISuccess(t, create)

	var site fallbackdomain.Site
	if err := dbsqlite.DB().Preload("Pages").First(&site).Error; err != nil {
		t.Fatalf("created site: %v", err)
	}
	if len(site.Pages) == 0 {
		t.Fatalf("created site has no default pages")
	}

	healthBefore := performFallbackAPIRequest(router, http.MethodGet, "/api/components/fallback-html/health", "")
	before := assertFallbackAPIObj[fallbackservice.RuntimeHealth](t, healthBefore)
	if before.OK || before.Active {
		t.Fatalf("health before publish should be inactive: %#v", before)
	}

	publish := performFallbackAPIRequest(router, http.MethodPost, "/api/components/fallback-html/sites/"+uintString(site.ID)+"/publish", "")
	assertFallbackAPISuccess(t, publish)
	var activePublish fallbackdomain.Publish
	if err := dbsqlite.DB().Where("site_id = ? AND active = ?", site.ID, true).First(&activePublish).Error; err != nil {
		t.Fatalf("active publish: %v", err)
	}
	artifact := performFallbackAPIRequest(router, http.MethodGet, "/api/components/fallback-html/sites/"+uintString(site.ID)+"/artifact/"+activePublish.Version, "")
	if artifact.Code != http.StatusOK || artifact.Header().Get("Content-Type") != "application/gzip" || !strings.Contains(artifact.Header().Get("Content-Disposition"), ".tar.gz") || artifact.Body.Len() == 0 {
		t.Fatalf("artifact download = %d headers=%v len=%d", artifact.Code, artifact.Header(), artifact.Body.Len())
	}

	healthAfter := performFallbackAPIRequest(router, http.MethodGet, "/api/components/fallback-html/health", "")
	after := assertFallbackAPIObj[fallbackservice.RuntimeHealth](t, healthAfter)
	if !after.OK || !after.Active || after.HomeStatus != http.StatusOK || after.NotFoundStatus != http.StatusNotFound {
		t.Fatalf("health after publish = %#v", after)
	}
}

func TestValidateAssetUploadMultipartRequiresOneFileOnly(t *testing.T) {
	valid := &multipart.Form{File: map[string][]*multipart.FileHeader{"file": {{Filename: "asset.png"}}}}
	if err := validateAssetUploadMultipart(valid); err != nil {
		t.Fatalf("valid upload rejected: %v", err)
	}
	for name, form := range map[string]*multipart.Form{
		"missing":   {File: map[string][]*multipart.FileHeader{}},
		"duplicate": {File: map[string][]*multipart.FileHeader{"file": {{Filename: "a"}, {Filename: "b"}}}},
		"field":     {Value: map[string][]string{"extra": {"x"}}, File: map[string][]*multipart.FileHeader{"file": {{Filename: "a"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateAssetUploadMultipart(form); err == nil {
				t.Fatal("invalid multipart upload was accepted")
			}
		})
	}
}

func TestNodeRoutesStayBehindNodeOrchestratorComponent(t *testing.T) {
	router, _ := newFallbackAPITestRouter(t, true)
	create := performFallbackAPIRequest(router, http.MethodPost, "/api/components/fallback-html/sites", `{"name":"Public test","enabled":true}`)
	assertFallbackAPISuccess(t, create)
	var site fallbackdomain.Site
	if err := dbsqlite.DB().First(&site).Error; err != nil {
		t.Fatalf("created site: %v", err)
	}
	publish := performFallbackAPIRequest(router, http.MethodPost, "/api/components/fallback-html/sites/"+uintString(site.ID)+"/publish", "")
	assertFallbackAPISuccess(t, publish)
	var activePublish fallbackdomain.Publish
	if err := dbsqlite.DB().Where("site_id = ? AND active = ?", site.ID, true).First(&activePublish).Error; err != nil {
		t.Fatalf("active publish: %v", err)
	}
	paths := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/components/fallback-html/nodes", ""},
		{http.MethodPost, "/api/components/fallback-html/nodes", `{"nodeId":"node-ui","baseUrl":"https://node.example.com"}`},
		{http.MethodGet, "/api/components/fallback-html/sites/" + uintString(site.ID) + "/node-plan/" + activePublish.Version, ""},
		{http.MethodGet, "/api/components/fallback-html/sites/" + uintString(site.ID) + "/node-publications", ""},
		{http.MethodPost, "/api/components/fallback-html/sites/" + uintString(site.ID) + "/node-publications/" + activePublish.Version + "/apply", `{"nodeId":"node-api"}`},
	}
	for _, item := range paths {
		response := performFallbackAPIRequest(router, item.method, item.path, item.body)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s %s = %d, want 404", item.method, item.path, response.Code)
		}
	}
}

func TestRoutesReturnValidationErrorAndEnforceScope(t *testing.T) {
	router, _ := newFallbackAPITestRouter(t, true)
	create := performFallbackAPIRequest(router, http.MethodPost, "/api/components/fallback-html/sites", `{"name":"Public test","enabled":true}`)
	assertFallbackAPISuccess(t, create)
	runtimes := assertFallbackAPIObj[[]fallbackservice.RuntimeOption](t, performFallbackAPIRequest(router, http.MethodGet, "/api/components/fallback-html/runtimes", ""))
	if len(runtimes) != 1 || runtimes[0].ID != "gin" || runtimes[0].Status != "available" || runtimes[0].NodeSide {
		t.Fatalf("unexpected runtime options: %#v", runtimes)
	}

	var site fallbackdomain.Site
	if err := dbsqlite.DB().First(&site).Error; err != nil {
		t.Fatalf("created site: %v", err)
	}
	providerStatus := assertFallbackAPIObj[ProviderStatusView](t, performFallbackAPIRequest(router, http.MethodGet, "/api/components/fallback-html/sites/"+uintString(site.ID)+"/provider-status", ""))
	if providerStatus.TargetID != "site:"+uintString(site.ID) || providerStatus.EndpointMode != "UNKNOWN" ||
		providerStatus.Readiness != "UNKNOWN" || providerStatus.CapacitySlotsTotal != 4 {
		t.Fatalf("provider status=%#v", providerStatus)
	}
	imported := assertFallbackAPIObj[fallbackservice.SiteImportResult](t, performFallbackAPIRequest(router, http.MethodPost, "/api/components/fallback-html/sites/"+uintString(site.ID)+"/import", `{"schema":"solovey-ui/fallback-html-site/v1","pages":[{"path":"/","title":"Imported","body":"Hello"}]}`))
	if imported.Pages != 1 || imported.SiteID != site.ID {
		t.Fatalf("unexpected import result: %#v", imported)
	}
	rejected := performFallbackAPIRequest(router, http.MethodPost, "/api/components/fallback-html/sites/"+uintString(site.ID)+"/pages", `{"path":"/api/","title":"bad","body":"bad"}`)
	response := decodeFallbackAPIResponse(t, rejected)
	if rejected.Code != http.StatusBadRequest || response.Success || !strings.Contains(response.Msg, "reserved") {
		t.Fatalf("reserved path response = %d %#v", rejected.Code, response)
	}
	badTemplate := performFallbackAPIRequest(router, http.MethodPost, "/api/components/fallback-html/sites", `{"name":"Bad","templateId":"unknown"}`)
	response = decodeFallbackAPIResponse(t, badTemplate)
	if badTemplate.Code != http.StatusBadRequest || response.Success || !strings.Contains(response.Msg, "unknown fallback-html template") {
		t.Fatalf("unknown template response = %d %#v", badTemplate.Code, response)
	}

	deniedRouter, _ := newFallbackAPITestRouter(t, false)
	denied := performFallbackAPIRequest(deniedRouter, http.MethodGet, "/api/components/fallback-html/templates", "")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("scope denied status = %d, want 403", denied.Code)
	}
}

func TestLegacySelfStealRouteIsBoundedGoneAndMakesZeroWrites(t *testing.T) {
	router, _ := newFallbackAPITestRouter(t, true)
	db := dbsqlite.DB()
	if err := authority.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	site := fallbackdomain.Site{Name: "Historical", Enabled: true, Status: "draft", CreatedAt: 1, UpdatedAt: 1}
	if err := db.Create(&site).Error; err != nil {
		t.Fatal(err)
	}
	target := fallbackdomain.RuntimeTarget{SiteID: site.ID, Kind: "standalone", Listen: "127.0.0.1", Port: 8443, Runtime: "gin"}
	if err := db.Create(&target).Error; err != nil {
		t.Fatal(err)
	}
	before := fallbackMutationCounts(t, db)
	path := "/api/components/fallback-html/sites/" + uintString(site.ID) + "/self-steal/draft"
	valid := performFallbackAPIRequest(router, http.MethodPost, path, `{"profile":"vless-reality","privateKey":"do-not-echo","path":"/private/path"}`)
	malformed := performFallbackAPIRequest(router, http.MethodPost, path, `{`)
	repeated := performFallbackAPIRequest(router, http.MethodPost, path, `{`)
	for _, response := range []*httptest.ResponseRecorder{valid, malformed, repeated} {
		decoded := decodeFallbackAPIResponse(t, response)
		if response.Code != http.StatusGone || decoded.Success || decoded.Msg != legacySelfStealRetiredCode {
			t.Fatalf("tombstone response=%d %#v body=%s", response.Code, decoded, response.Body.String())
		}
		for _, forbidden := range []string{"do-not-echo", "/private/path", "privateKey"} {
			if strings.Contains(response.Body.String(), forbidden) {
				t.Fatalf("tombstone leaked %q: %s", forbidden, response.Body.String())
			}
		}
	}
	if malformed.Body.String() != repeated.Body.String() || valid.Body.String() != malformed.Body.String() {
		t.Fatalf("tombstone response is not deterministic: valid=%s malformed=%s repeated=%s", valid.Body, malformed.Body, repeated.Body)
	}
	if after := fallbackMutationCounts(t, db); after != before {
		t.Fatalf("legacy tombstone mutated database: before=%#v after=%#v", before, after)
	}
	if response := performFallbackAPIRequest(router, http.MethodGet, path, ""); response.Code != http.StatusNotFound {
		t.Fatalf("unsupported method status=%d, want 404", response.Code)
	}
	deniedRouter, _ := newFallbackAPITestRouter(t, false)
	if response := performFallbackAPIRequest(deniedRouter, http.MethodPost, path, `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("scope denial status=%d, want 403", response.Code)
	}
	oversized := performFallbackAPIRequest(router, http.MethodPost, path, strings.Repeat("x", legacySelfStealBodyLimit+1))
	if oversized.Code != http.StatusRequestEntityTooLarge || strings.Contains(oversized.Body.String(), strings.Repeat("x", 32)) {
		t.Fatalf("oversized body response=%d %s", oversized.Code, oversized.Body.String())
	}
}

type mutationCounts struct {
	HistoricalDrafts int64
	CoreDrafts       int64
	Targets          int64
	TLSRows          int64
	Inbounds         int64
	Reservations     int64
}

func fallbackMutationCounts(t *testing.T, db *gorm.DB) mutationCounts {
	t.Helper()
	var result mutationCounts
	for modelValue, destination := range map[any]*int64{
		&fallbackdomain.SelfStealDraft{}: &result.HistoricalDrafts,
		&model.InboundDraft{}:            &result.CoreDrafts,
		&fallbackdomain.RuntimeTarget{}:  &result.Targets,
		&model.Tls{}:                     &result.TLSRows,
		&model.Inbound{}:                 &result.Inbounds,
		&authority.ReservationModel{}:    &result.Reservations,
	} {
		if err := db.Model(modelValue).Count(destination).Error; err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func TestCreateSiteFromTemplateRoute(t *testing.T) {
	router, _ := newFallbackAPITestRouter(t, true)
	create := performFallbackAPIRequest(router, http.MethodPost, "/api/components/fallback-html/templates/knowledge-base/create-site", "")
	site := assertFallbackAPIObj[fallbackdomain.Site](t, create)
	if site.TemplateID != "knowledge-base" || !strings.Contains(site.Name, "Knowledge") {
		t.Fatalf("template-created site = %#v", site)
	}

	rejected := performFallbackAPIRequest(router, http.MethodPost, "/api/components/fallback-html/templates/unknown/create-site", "")
	response := decodeFallbackAPIResponse(t, rejected)
	if rejected.Code != http.StatusBadRequest || response.Success || !strings.Contains(response.Msg, "unknown fallback-html template") {
		t.Fatalf("unknown template response = %d %#v", rejected.Code, response)
	}
}

func newFallbackAPITestRouter(t *testing.T, allowed bool) (*gin.Engine, *fallbackservice.Service) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := openFallbackAPITestDB(t)
	setFallbackAPISetting(t, db, "webPath", "/secret-panel/")
	runtime := fallbackservice.NewRuntime()
	t.Cleanup(runtime.Stop)
	service := fallbackservice.New(db, runtime)
	router := gin.New()
	RegisterRoutes(router.Group("/api"), Deps{
		Service: service,
		ProviderStatus: func(_ context.Context, siteID uint) (ProviderStatusView, error) {
			return ProviderStatusView{
				TargetID: siteIDString(siteID), EndpointMode: "UNKNOWN", Readiness: "UNKNOWN",
				HealthFreshness: "UNKNOWN", CapacityState: "UNKNOWN", CapacitySlotsTotal: 4,
				Reservations: []ProviderReservationStateView{}, ReasonCodes: []string{"target_not_published_or_ready"},
			}, nil
		},
		RequireScope: func(c *gin.Context, _ string, _ ...string) bool {
			if allowed {
				return true
			}
			c.AbortWithStatus(http.StatusForbidden)
			return false
		},
		Actor: func(*gin.Context) string { return "tester" },
		JSONObj: func(c *gin.Context, value interface{}, err error) {
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"success": true, "obj": value})
		},
		JSONMsg: func(c *gin.Context, msg string, err error) {
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"success": true, "msg": msg})
		},
	})
	return router, service
}

func siteIDString(siteID uint) string {
	return "site:" + strconv.FormatUint(uint64(siteID), 10)
}

func openFallbackAPITestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(filepath.Join(dir, "s-ui.db")); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	db := dbsqlite.DB()
	if err := fallbackdomain.EnsureSchema(db); err != nil {
		t.Fatalf("fallback schema: %v", err)
	}
	return db
}

func setFallbackAPISetting(t *testing.T, db *gorm.DB, key string, value string) {
	t.Helper()
	if err := db.Where("key = ?", key).Delete(&model.Setting{}).Error; err != nil {
		t.Fatalf("delete setting %s: %v", key, err)
	}
	if err := db.Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
		t.Fatalf("create setting %s: %v", key, err)
	}
}

func performFallbackAPIRequest(router http.Handler, method string, path string, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(response, request)
	return response
}

func assertFallbackAPISuccess(t *testing.T, response *httptest.ResponseRecorder) apiResponse {
	t.Helper()
	decoded := decodeFallbackAPIResponse(t, response)
	if response.Code != http.StatusOK || !decoded.Success {
		t.Fatalf("API response = %d %#v body=%s", response.Code, decoded, response.Body.String())
	}
	return decoded
}

func assertFallbackAPIObj[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	decoded := assertFallbackAPISuccess(t, response)
	var value T
	if err := json.Unmarshal(decoded.Obj, &value); err != nil {
		t.Fatalf("decode obj: %v body=%s", err, response.Body.String())
	}
	return value
}

func decodeFallbackAPIResponse(t *testing.T, response *httptest.ResponseRecorder) apiResponse {
	t.Helper()
	var decoded apiResponse
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode API response: %v body=%s", err, response.Body.String())
	}
	return decoded
}

func uintString(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
