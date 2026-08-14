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
	"sync/atomic"
	"testing"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionfronting "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/fronting"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type testEnvelope struct {
	Success bool            `json:"success"`
	Msg     string          `json:"msg"`
	Obj     json.RawMessage `json:"obj"`
}

type auditEvent struct {
	Name    string
	Details map[string]any
}

type fixtureContributor struct{ owner string }

func (c fixtureContributor) Owner() string { return c.owner }
func (c fixtureContributor) ListProtectableResources(_ context.Context) ([]hostresources.ProtectableResource, error) {
	return []hostresources.ProtectableResource{{
		ID: "fixture:listener:one", Kind: "component_listener", Owner: c.owner, Name: "Fixture listener",
		Protocol: "stream", Listen: "127.0.0.1", Port: 9443, TLS: true, Source: "fixture", Fingerprint: "fixture-revision",
		Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, ConfigRevision: strings.Repeat("c", 64)},
	}}, nil
}

var fixtureContributorSequence atomic.Uint64

func newProtectionAPIRouter(t *testing.T, scope string) (*gin.Engine, *[]auditEvent) {
	router, audits, _ := newProtectionAPIRouterWithOperations(t, scope)
	return router, audits
}

func newProtectionAPIRouterWithOperations(t *testing.T, scope string) (*gin.Engine, *[]auditEvent, *protectionoperations.Manager) {
	router, audits, manager, _ := newProtectionAPIRouterWithFronting(t, scope, protectionfronting.NewNginxAdapter())
	return router, audits, manager
}

func newProtectionAPIRouterWithFronting(t *testing.T, scope string, fronting protectionfronting.FrontingController) (*gin.Engine, *[]auditEvent, *protectionoperations.Manager, *protectionrepository.Repository) {
	router, audits, manager, repository, _ := newProtectionAPIRouterWithDB(t, scope, fronting)
	return router, audits, manager, repository
}

func newProtectionAPIRouterWithDB(t *testing.T, scope string, fronting protectionfronting.FrontingController) (*gin.Engine, *[]auditEvent, *protectionoperations.Manager, *protectionrepository.Repository, *gorm.DB) {
	return newProtectionAPIRouterWithDBAndSemantic(t, scope, fronting, &frontingSemanticStub{})
}

func newProtectionAPIRouterWithDBAndSemantic(t *testing.T, scope string, fronting protectionfronting.FrontingController, frontingV2 frontingSemanticService) (*gin.Engine, *[]auditEvent, *protectionoperations.Manager, *protectionrepository.Repository, *gorm.DB) {
	return newProtectionAPIRouterWithLocalProxy(t, scope, fronting, frontingV2, nil)
}

func newProtectionAPIRouterWithLocalProxy(t *testing.T, scope string, fronting protectionfronting.FrontingController, frontingV2 frontingSemanticService, localProxy localProxyService) (*gin.Engine, *[]auditEvent, *protectionoperations.Manager, *protectionrepository.Repository, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "server-protection-api.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := protectionrepository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	owner := "fixture-api-" + strconv.FormatUint(fixtureContributorSequence.Add(1), 10)
	unregister, err := hostresources.Register(fixtureContributor{owner: owner})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unregister)
	audits := &[]auditEvent{}
	repository := protectionrepository.New(db)
	manager := protectionoperations.NewManager(repository, protectionoperations.Options{
		InstanceID: "api-test", PID: 4242,
		Audit: func(_ context.Context, event protectionoperations.AuditEvent) error {
			*audits = append(*audits, auditEvent{Name: event.Event, Details: event.Details})
			return nil
		},
	})
	t.Cleanup(func() { _ = manager.Stop(context.Background()) })
	router := gin.New()
	RegisterRoutes(router.Group("/api"), Deps{
		Repository: repository,
		Operations: manager,
		Fronting:   fronting,
		FrontingV2: frontingV2,
		LocalProxy: localProxy,
		RequireScope: func(c *gin.Context, _ string, allowed ...string) bool {
			for _, candidate := range allowed {
				if candidate == scope {
					return true
				}
			}
			c.AbortWithStatus(http.StatusForbidden)
			return false
		},
		Actor: func(*gin.Context) string { return "tester" },
		Audit: func(_ *gin.Context, _ string, event string, _ string, _ string, details map[string]any) {
			copy := make(map[string]any, len(details))
			for key, value := range details {
				copy[key] = value
			}
			*audits = append(*audits, auditEvent{Name: event, Details: copy})
		},
		JSONObj: func(c *gin.Context, value interface{}, err error) {
			c.JSON(http.StatusOK, gin.H{"success": err == nil, "msg": errorString(err), "obj": value})
		},
		JSONMsg: func(c *gin.Context, msg string, err error) {
			c.JSON(http.StatusOK, gin.H{"success": err == nil, "msg": msg + errorString(err), "obj": nil})
		},
	})
	return router, audits, manager, repository, db
}

type frontingSemanticStub struct{}

func (*frontingSemanticStub) Status(context.Context) (protectionfronting.FrontingStatusPageV2, error) {
	return protectionfronting.FrontingStatusPageV2{Items: []protectionfronting.FrontingStatusV2{}, GeneratedAt: 1}, nil
}
func (*frontingSemanticStub) Preview(context.Context, protectionfronting.FrontingPreviewRequestV2) (protectionfronting.FrontingStrategyPlanV2, error) {
	return protectionfronting.FrontingStrategyPlanV2{}, &protectionfronting.SemanticErrorV2{Code: "validation_unavailable"}
}
func (*frontingSemanticStub) Prepare(context.Context, protectionfronting.FrontingPrepareRequestV2, string) (protectionfronting.FrontingOperationViewV2, error) {
	return protectionfronting.FrontingOperationViewV2{}, &protectionfronting.SemanticErrorV2{Code: "validation_unavailable"}
}
func (*frontingSemanticStub) Apply(context.Context, protectionfronting.FrontingApplyRequestV2) (protectionfronting.FrontingOperationViewV2, error) {
	return protectionfronting.FrontingOperationViewV2{}, &protectionfronting.SemanticErrorV2{Code: "validation_unavailable"}
}
func (*frontingSemanticStub) Rollback(context.Context, protectionfronting.FrontingRollbackRequestV2) (protectionfronting.FrontingOperationViewV2, error) {
	return protectionfronting.FrontingOperationViewV2{}, &protectionfronting.SemanticErrorV2{Code: "validation_unavailable"}
}
func (*frontingSemanticStub) Operation(context.Context, string) (protectionfronting.FrontingOperationViewV2, error) {
	return protectionfronting.FrontingOperationViewV2{}, &protectionfronting.SemanticErrorV2{Code: "operation_not_found"}
}
func (*frontingSemanticStub) Recovery(context.Context, string) (protectionfronting.FrontingRecoveryStatusV2, error) {
	return protectionfronting.FrontingRecoveryStatusV2{}, &protectionfronting.SemanticErrorV2{Code: "operation_not_found"}
}

func requestProtectionAPI(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeProtectionObject(t *testing.T, recorder *httptest.ResponseRecorder, value any) {
	t.Helper()
	envelope := assertProtectionSuccess(t, recorder)
	if err := json.Unmarshal(envelope.Obj, value); err != nil {
		t.Fatalf("decode response object: %v body=%s", err, recorder.Body.String())
	}
}

func assertProtectionSuccess(t *testing.T, recorder *httptest.ResponseRecorder) testEnvelope {
	t.Helper()
	var envelope testEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v body=%s", err, recorder.Body.String())
	}
	if recorder.Code != http.StatusOK || !envelope.Success {
		t.Fatalf("response = %d %#v body=%s", recorder.Code, envelope, recorder.Body.String())
	}
	return envelope
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
