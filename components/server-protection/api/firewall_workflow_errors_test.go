//go:build !minimal

package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionfirewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
	"github.com/gin-gonic/gin"
)

func TestIncompleteResourceInventoryIsFailClosed(t *testing.T) {
	for name, inventory := range map[string]protectionresources.InventorySnapshot{
		"empty": {},
		"partial with contributor error": {
			Resources: []hostresources.ProtectableResource{{ID: "core:panel:web", Protocol: "http", Port: 443}},
			Errors:    []hostresources.ResourceError{{Owner: "core", Message: "redacted fixture"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := firewallInventoryReady(inventory); err == nil || !strings.Contains(err.Error(), "inventory_incomplete") {
				t.Fatalf("incomplete inventory was accepted: %v", err)
			}
		})
	}
}

func TestFirewallWorkflowErrorsHaveExactConflictCodes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for name, fixture := range map[string]struct {
		err  error
		code string
	}{
		"stale revision":                     {protectionfirewall.ErrPlanRevision, "revision_conflict"},
		"unsafe inventory":                   {protectionfirewall.ErrUnsafeResource, "unsafe_resource_inventory"},
		"helper revision":                    {protectionfirewall.ErrHelperRevision, "helper_revision_conflict"},
		"missing helper":                     {protectionfirewall.ErrMissingCapability, "missing_capability"},
		"verify mismatch":                    {protectionfirewall.ErrApplyVerify, "apply_verify_mismatch"},
		"health failure":                     {protectionfirewall.ErrHealthFailed, "health_failed"},
		"rollback health":                    {protectionfirewall.ErrRollbackHealth, "rollback_health_failed"},
		"fencing mismatch":                   {protectionoperations.ErrFenced, "operation_fenced"},
		"abandoned operation":                {protectionoperations.ErrConflict, "operation_state_conflict"},
		"real rollback failure after health": {errors.Join(protectionfirewall.ErrHealthFailed, protectionfirewall.ErrRollbackFailed), "rollback_failed"},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			writeFirewallWorkflowError(ctx, fixture.err)
			if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), `"code":"`+fixture.code+`"`) {
				t.Fatalf("response=%d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestUnknownWorkflowErrorsDoNotExposeRulesetPayload(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	writeFirewallWorkflowError(ctx, errors.New("private-path backend ruleset payload"))
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), `"code":"operation_failed"`) || strings.Contains(recorder.Body.String(), "private-path") {
		t.Fatalf("unsafe error response=%d %s", recorder.Code, recorder.Body.String())
	}
}
