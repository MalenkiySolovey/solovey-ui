//go:build !minimal

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

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
	plan := assertFallbackAPIObj[fallbackservice.NodePublishPlan](t, performFallbackAPIRequest(router, http.MethodGet, "/api/components/fallback-html/sites/"+uintString(site.ID)+"/node-plan/"+activePublish.Version+"?nodeId=node-eu-1", ""))
	if plan.Schema == "" || plan.NodeID != "node-eu-1" || plan.Artifact.Sha256 == "" || plan.Signature.Mode == "" {
		t.Fatalf("unexpected node publish plan: %#v", plan)
	}
	publications := assertFallbackAPIObj[[]fallbackservice.NodePublicationView](t, performFallbackAPIRequest(router, http.MethodGet, "/api/components/fallback-html/sites/"+uintString(site.ID)+"/node-publications", ""))
	if len(publications) != 0 {
		t.Fatalf("node publications should be empty before orchestrator records status: %#v", publications)
	}

	healthAfter := performFallbackAPIRequest(router, http.MethodGet, "/api/components/fallback-html/health", "")
	after := assertFallbackAPIObj[fallbackservice.RuntimeHealth](t, healthAfter)
	if !after.OK || !after.Active || after.HomeStatus != http.StatusOK || after.NotFoundStatus != http.StatusNotFound {
		t.Fatalf("health after publish = %#v", after)
	}
}

func TestNodePublicationApplyRoute(t *testing.T) {
	router, service := newFallbackAPITestRouter(t, true)
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
	service.SetNodeClient(apiNodeClientFunc{
		validate: func(_ context.Context, target fallbackservice.NodeApplyTarget, artifact fallbackservice.ArtifactArchive) (fallbackservice.NodeRuntimeStatus, error) {
			if target.NodeID != "node-api" || target.Runtime != "gin" || artifact.Sha256 == "" {
				t.Fatalf("unexpected validate input target=%#v artifact=%#v", target, artifact)
			}
			return fallbackservice.NodeRuntimeStatus{OK: true, SiteID: uintString(site.ID), Version: activePublish.Version, ArtifactSha256: artifact.Sha256}, nil
		},
		apply: func(_ context.Context, target fallbackservice.NodeApplyTarget, artifact fallbackservice.ArtifactArchive) (fallbackservice.NodeRuntimeStatus, error) {
			return fallbackservice.NodeRuntimeStatus{SiteID: uintString(site.ID), Version: activePublish.Version, Runtime: target.Runtime, Status: "applied", ArtifactSha256: artifact.Sha256, AppliedAt: 77}, nil
		},
	})
	response := performFallbackAPIRequest(router, http.MethodPost, "/api/components/fallback-html/sites/"+uintString(site.ID)+"/node-publications/"+activePublish.Version+"/apply", `{"nodeId":"node-api","baseUrl":"https://node.example.com","runtime":"gin"}`)
	result := assertFallbackAPIObj[fallbackservice.NodeApplyResult](t, response)
	if result.Status.Status != "active" || result.Status.NodeID != "node-api" || result.Status.AppliedAt != 77 {
		t.Fatalf("unexpected node apply API result: %#v", result)
	}
}

func TestNodeEndpointRegistryRoutes(t *testing.T) {
	router, _ := newFallbackAPITestRouter(t, true)
	saved := assertFallbackAPIObj[fallbackservice.NodeEndpointView](t, performFallbackAPIRequest(router, http.MethodPost, "/api/components/fallback-html/nodes", `{"nodeId":"node-ui","baseUrl":"https://node.example.com","runtime":"nginx","sharedSecret":"secret"}`))
	if saved.NodeID != "node-ui" || saved.BaseURL != "https://node.example.com" || saved.Runtime != "nginx" || !saved.Enabled || !saved.HasSharedSecret {
		t.Fatalf("unexpected saved node endpoint: %#v", saved)
	}
	nodes := assertFallbackAPIObj[[]fallbackservice.NodeEndpointView](t, performFallbackAPIRequest(router, http.MethodGet, "/api/components/fallback-html/nodes", ""))
	if len(nodes) != 1 || nodes[0].NodeID != "node-ui" {
		t.Fatalf("unexpected node endpoints: %#v", nodes)
	}
	assertFallbackAPISuccess(t, performFallbackAPIRequest(router, http.MethodDelete, "/api/components/fallback-html/nodes/node-ui", ""))
	nodes = assertFallbackAPIObj[[]fallbackservice.NodeEndpointView](t, performFallbackAPIRequest(router, http.MethodGet, "/api/components/fallback-html/nodes", ""))
	if len(nodes) != 0 {
		t.Fatalf("node endpoint was not deleted: %#v", nodes)
	}
}

func TestRoutesReturnValidationErrorAndEnforceScope(t *testing.T) {
	router, _ := newFallbackAPITestRouter(t, true)
	create := performFallbackAPIRequest(router, http.MethodPost, "/api/components/fallback-html/sites", `{"name":"Public test","enabled":true}`)
	assertFallbackAPISuccess(t, create)
	runtimes := assertFallbackAPIObj[[]fallbackservice.RuntimeOption](t, performFallbackAPIRequest(router, http.MethodGet, "/api/components/fallback-html/runtimes", ""))
	if len(runtimes) != 3 || runtimes[0].ID != "gin" || runtimes[0].Status != "available" || runtimes[1].Status != "unavailable" || !runtimes[1].NodeSide {
		t.Fatalf("unexpected runtime options: %#v", runtimes)
	}

	var site fallbackdomain.Site
	if err := dbsqlite.DB().First(&site).Error; err != nil {
		t.Fatalf("created site: %v", err)
	}
	draft := assertFallbackAPIObj[fallbackservice.SelfStealDraftView](t, performFallbackAPIRequest(router, http.MethodPost, "/api/components/fallback-html/sites/"+uintString(site.ID)+"/self-steal/draft", `{}`))
	if draft.Status != "blocked" || !draft.Payload.NoApply || draft.Payload.RequiresCapability != "inbound-draft" {
		t.Fatalf("unexpected self-steal API draft: %#v", draft)
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

type apiNodeClientFunc struct {
	validate func(context.Context, fallbackservice.NodeApplyTarget, fallbackservice.ArtifactArchive) (fallbackservice.NodeRuntimeStatus, error)
	apply    func(context.Context, fallbackservice.NodeApplyTarget, fallbackservice.ArtifactArchive) (fallbackservice.NodeRuntimeStatus, error)
}

func (n apiNodeClientFunc) Validate(ctx context.Context, target fallbackservice.NodeApplyTarget, artifact fallbackservice.ArtifactArchive) (fallbackservice.NodeRuntimeStatus, error) {
	return n.validate(ctx, target, artifact)
}

func (n apiNodeClientFunc) Apply(ctx context.Context, target fallbackservice.NodeApplyTarget, artifact fallbackservice.ArtifactArchive) (fallbackservice.NodeRuntimeStatus, error) {
	return n.apply(ctx, target, artifact)
}
