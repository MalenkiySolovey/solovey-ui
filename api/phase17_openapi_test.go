package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pressuredomain "github.com/MalenkiySolovey/solovey-ui/internal/ops/resourcepressure"
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

type phase17OpenAPISpec struct {
	OpenAPI    string                                 `yaml:"openapi"`
	Paths      map[string]map[string]phase17Operation `yaml:"paths"`
	Components struct {
		Schemas map[string]phase17Schema `yaml:"schemas"`
	} `yaml:"components"`
}

type phase17Operation struct {
	OperationID string           `yaml:"operationId"`
	RequestBody map[string]any   `yaml:"requestBody"`
	Parameters  []map[string]any `yaml:"parameters"`
}

type phase17Schema struct {
	AdditionalProperties *bool                    `yaml:"additionalProperties"`
	Properties           map[string]phase17Schema `yaml:"properties"`
}

func TestPhase17OpenAPIInventoryAndStrictRequests(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "docs", "phase17-operations-api.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var spec phase17OpenAPISpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.OpenAPI != "3.1.0" {
		t.Fatalf("openapi=%q, want 3.1.0", spec.OpenAPI)
	}

	initSessionTestDB(t)
	prepareComponentRouteMetadata(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := &APIHandler{}
	handler.initRouter(router.Group("/api"))

	actual := map[string]map[string]bool{}
	for _, route := range router.Routes() {
		if !phase17ContractRoute(route.Path) {
			continue
		}
		path := strings.ReplaceAll(route.Path, ":operationId", "{operationId}")
		if actual[path] == nil {
			actual[path] = map[string]bool{}
		}
		actual[path][strings.ToLower(route.Method)] = true
	}

	operationIDs := map[string]string{}
	for path, methods := range actual {
		operations, ok := spec.Paths[path]
		if !ok {
			t.Errorf("OpenAPI path %q is missing", path)
			continue
		}
		for method := range methods {
			operation, ok := operations[method]
			if !ok || operation.OperationID == "" {
				t.Errorf("OpenAPI operation %s %s is missing or unnamed", strings.ToUpper(method), path)
			}
			if previous, duplicate := operationIDs[operation.OperationID]; operation.OperationID != "" && duplicate {
				t.Errorf("OpenAPI operationId %q is duplicated by %s %s and %s", operation.OperationID, strings.ToUpper(method), path, previous)
			} else if operation.OperationID != "" {
				operationIDs[operation.OperationID] = strings.ToUpper(method) + " " + path
			}
			if method == strings.ToLower(http.MethodPost) && operation.RequestBody == nil {
				t.Errorf("OpenAPI mutation %s %s has no request body contract", strings.ToUpper(method), path)
			}
		}
	}
	for path, operations := range spec.Paths {
		if !phase17ContractRoute(path) {
			continue
		}
		for method := range operations {
			if !actual[path][method] {
				t.Errorf("OpenAPI declares nonexistent operation %s %s", strings.ToUpper(method), path)
			}
		}
	}

	strictSchemas := []string{
		"SessionRevokeRequest", "StepUpBodyRequest", "PasswordTransitionRequest", "PasswordChangeRequest",
		"StepUpIssueRequest", "MFACodeRequest", "AcknowledgementRequest", "RecoveryTransitionRequest",
		"SSHDesiredPolicy", "SSHPreviewRequest", "SSHStartRequest", "SSHReconnectConfirmationRequest", "RevisionRequest",
		"DeploymentPreviewRequest", "DeploymentStartRequest",
		"UpdateCheckRequest", "UpdatePrepareRequest", "UpdateOperationRequest",
		"UpdateRevisionRequest",
		"DropPreviewRequest", "DropExecuteRequest", "RestoreRehearsalRequest", "RestoreExecuteRequest",
	}
	for _, name := range strictSchemas {
		schema, ok := spec.Components.Schemas[name]
		if !ok {
			t.Errorf("request schema %q is missing", name)
			continue
		}
		if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
			t.Errorf("request schema %q must reject unknown fields", name)
		}
		for property := range schema.Properties {
			if forbiddenOperationalProperty(property) {
				t.Errorf("request schema %q exposes forbidden raw control field %q", name, property)
			}
		}
	}

	for _, path := range []string{
		"/api/v1/operations/ssh/candidate",
		"/api/v1/operations/ssh/candidate/{operationId}/reconnect/confirm",
		"/api/v1/operations/ssh/candidate/{operationId}/rollback",
		"/api/v1/operations/deployment/migration",
		"/api/v1/operations/deployment/migration/{operationId}/confirm",
		"/api/v1/operations/deployment/migration/{operationId}/rollback",
		"/api/v1/operations/update/prepare",
		"/api/v1/operations/update/preflight",
		"/api/v1/operations/update/activate",
		"/api/v1/operations/update/rollback",
		"/api/v1/operations/update/operations/{operationId}/activate",
		"/api/v1/operations/update/operations/{operationId}/rollback",
		"/api/v1/operations/data/drop",
		"/api/v1/operations/data/restore",
	} {
		operation := spec.Paths[path]["post"]
		found := false
		for _, parameter := range operation.Parameters {
			if parameter["$ref"] == "#/components/parameters/StepUpToken" {
				found = true
			}
		}
		if !found {
			t.Errorf("sensitive mutation POST %s does not document the step-up header", path)
		}
	}
}

func phase17ContractRoute(path string) bool {
	return path == "/api/getdb" || strings.HasPrefix(path, "/api/v1/security/") ||
		strings.HasPrefix(path, "/api/v1/operations/")
}

func forbiddenOperationalProperty(property string) bool {
	switch strings.ToLower(property) {
	case "url", "repository", "publickey", "path", "filepath", "package", "command",
		"argv", "environment", "unit", "dockerendpoint", "pid", "sql", "table", "migration":
		return true
	default:
		return false
	}
}

func TestOwnerBackupBlockersAreNarrow(t *testing.T) {
	if ownerBackupBlocked([]string{"owner_enabled_state_unavailable:server-protection"}) {
		t.Fatal("an enabled-state observation failure must not exclude installed owner data from backup")
	}
	if !ownerBackupBlocked([]string{"installed_owner_manifest_unavailable"}) {
		t.Fatal("an unreadable installed-owner manifest must fail backup posture closed")
	}
	if !ownerBackupBlocked([]string{"installed_owner_unavailable:server-protection"}) {
		t.Fatal("an unavailable installed owner must fail backup posture closed")
	}
}

func TestPressureCleanupPostureIsPreviewOnlyAndNonDestructive(t *testing.T) {
	projection := pressureCleanupProjection(pressuredomain.Snapshot{
		State: pressuredomain.StateCritical, Revision: 7, ReasonCodes: []string{"CRITICAL:filesystem.data.free_ratio"},
	})
	if projection["state"] != "OPERATOR_ACTION_REQUIRED" || projection["previewOnly"] != true {
		t.Fatalf("unexpected critical cleanup posture: %#v", projection)
	}
	actions, ok := projection["automaticActions"].([]gin.H)
	if !ok || len(actions) == 0 {
		t.Fatalf("automatic action projection is missing: %#v", projection)
	}
	for _, action := range actions {
		if destructive, _ := action["destructive"].(bool); destructive {
			t.Fatalf("pressure cleanup authorized a destructive action: %#v", action)
		}
	}
}

func TestPressureHistoryQueryIsBoundedAndRejectsUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest("GET", "/?after=12&limit=200", nil)
	after, limit, ok := boundedPressureHistoryQuery(context)
	if !ok || after != 12 || limit != 200 {
		t.Fatalf("history query after=%d limit=%d ok=%v", after, limit, ok)
	}

	recorder = httptest.NewRecorder()
	context, _ = gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest("GET", "/?limit=201&rawCommand=1", nil)
	if _, _, ok := boundedPressureHistoryQuery(context); ok || recorder.Code != 400 {
		t.Fatalf("unknown or oversized history query was accepted: code=%d", recorder.Code)
	}
}
