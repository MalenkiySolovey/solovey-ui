package config

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"

	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/gin-gonic/gin"
)

func TestCoreEditorContractIsScopedAndOwnerDerived(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, allowed := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		called := false
		h := NewHandler(Deps{RequireScope: func(_ *gin.Context, resource string, _ ...string) bool {
			if resource != "config" {
				t.Fatal("wrong scope")
			}
			return allowed
		}, JSONObj: func(_ *gin.Context, obj any, err error) {
			called = true
			raw, _ := json.Marshal(obj)
			contract := singboxconfig.EditorContract()
			contract.TUNDNSUnavailableModes = entityinbounds.TUNDNSUnavailableModes()
			expected, _ := json.Marshal(contract)
			if string(raw) != string(expected) || err != nil {
				t.Fatal("editor diverged from owner")
			}
		}})
		h.GetEditorContract(ctx)
		if called != allowed {
			t.Fatal("authorization bypass")
		}
	}
}

func TestEditorRoutesMountOnSharedAPIRouter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router.Group("/api"), Deps{RequireScope: func(*gin.Context, string, ...string) bool { return true }, JSONObj: func(c *gin.Context, obj any, err error) {
		c.JSON(http.StatusOK, gin.H{"success": err == nil, "obj": obj})
	}})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/editor-contract", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "directHttpClient") {
		t.Fatal("shared editor facts were not mounted on the actual API route")
	}
	request := httptest.NewRequest(http.MethodPost, "/api/compatibility-preview", strings.NewReader(`{"subscriptionTemplate":{"rule_set":[]}}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"blocked":false`) {
		t.Fatal("compatibility preview was not mounted on the actual JSON API route")
	}
}

func TestCompatibilityPreviewAuthorizationPrecedesReadAndParse(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/compatibility-preview", strings.NewReader("invalid"))
	h := NewHandler(Deps{RequireScope: func(*gin.Context, string, ...string) bool { return false }, JSONObj: func(*gin.Context, any, error) { t.Fatal("read before authorization") }, JSONMsg: func(*gin.Context, string, error) { t.Fatal("parse before authorization") }})
	h.PreviewCompatibility(ctx)
}

func TestSubscriptionCompatibilityPreviewIsPureAndPreservesManualSource(t *testing.T) {
	for _, test := range []struct {
		template string
		blocked  bool
	}{
		{`{"rule_set":[{"tag":"operator","type":"remote","url":"https://operator.example/custom.srs","download_detour":"direct"}]}`, false},
		{`{"rule_set":[{"tag":"operator","type":"remote","url":"https://operator.example/custom.srs","download_detour":"direct","http_client":{"engine":"go","version":2}}]}`, true},
	} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/compatibility-preview", strings.NewReader(`{"subscriptionTemplate":`+test.template+`}`))
		called := false
		h := NewHandler(Deps{RequireScope: func(*gin.Context, string, ...string) bool { return true }, JSONObj: func(_ *gin.Context, obj any, err error) {
			called = true
			raw, _ := json.Marshal(obj)
			var result struct {
				Blocked  bool
				Template json.RawMessage `json:"subscriptionTemplate"`
				Findings []diagnostics.Finding
			}
			if err != nil || json.Unmarshal(raw, &result) != nil || result.Blocked != test.blocked {
				t.Fatal("preview classification lost")
			}
			if test.blocked && (len(result.Template) > 0 || diagnostics.FirstError(result.Findings) == nil) {
				t.Fatal("manual preview exposed candidate")
			}
			if !test.blocked && (!strings.Contains(string(result.Template), "custom.srs") || !strings.Contains(string(result.Template), "http_client")) {
				t.Fatal("preview lost custom policy")
			}
		}})
		h.PreviewCompatibility(ctx)
		if !called {
			t.Fatal("preview did not return")
		}
	}
}

func TestSemanticSaveReasonAndPathSurviveWrapping(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	f := diagnostics.Finding{Path: "dns.rules[2].match_response", Code: "dns_evaluate_reference_missing", Severity: diagnostics.Error, Message: "Choose an earlier evaluate tag."}
	var object map[string]any
	h := NewHandler(Deps{JSONObj: func(_ *gin.Context, obj any, err error) {
		if err == nil {
			t.Fatal("lost error")
		}
		object = obj.(map[string]any)
	}})
	if !h.handleCapabilitySaveError(ctx, fmt.Errorf("save: %w", &diagnostics.Rejection{Finding: f})) || object["reason"] != f.Code {
		t.Fatal("lost semantic classification")
	}
	findings := object["findings"].([]diagnostics.Finding)
	if len(findings) != 1 || findings[0] != f {
		t.Fatal("lost structural path")
	}
}
