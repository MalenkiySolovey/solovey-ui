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

func TestCompatibilityPreviewAuthorizationPrecedesReadAndParse(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/compatibility-preview", strings.NewReader("invalid"))
	h := NewHandler(Deps{RequireScope: func(*gin.Context, string, ...string) bool { return false }, JSONObj: func(*gin.Context, any, error) { t.Fatal("read before authorization") }, JSONMsg: func(*gin.Context, string, error) { t.Fatal("parse before authorization") }})
	h.PreviewCompatibility(ctx)
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
