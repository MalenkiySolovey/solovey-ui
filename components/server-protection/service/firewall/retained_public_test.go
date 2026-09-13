//go:build !minimal

package firewall_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionapi "github.com/MalenkiySolovey/solovey-ui/components/server-protection/api"
	firewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
	operations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	repository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	resources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
	"github.com/gin-gonic/gin"
)

type publicFixtureHealth struct {
	id    string
	check func(context.Context) componenthealth.Result
}

func (h publicFixtureHealth) ResourceID() string                               { return h.id }
func (h publicFixtureHealth) Check(ctx context.Context) componenthealth.Result { return h.check(ctx) }

func TestRetainedAuthorityPublicRollbackLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	calls := 0
	firewall.RunRetainedPublicLifecycleTest(t, func(workflow firewall.Workflow, repo *repository.Repository, id string) (firewall.Result, error) {
		calls++
		// API's health gate uses the registry and the shared SSH owner. Install
		// this fixture's same resource providers; retain the real gate itself.
		oldHealth := componenthealth.Default
		componenthealth.Default = componenthealth.NewRegistry()
		defer func() { componenthealth.Default = oldHealth }()
		inventory := resources.Snapshot(t.Context(), true)
		for _, resource := range inventory.Resources {
			_, err := componenthealth.Register(publicFixtureHealth{id: resource.ID, check: func(ctx context.Context) componenthealth.Result {
				results := workflow.Health(ctx, []hostresources.ProtectableResource{resource})
				if len(results) != 1 {
					return componenthealth.Result{ResourceID: resource.ID, Status: componenthealth.StatusDegraded}
				}
				return results[0]
			}})
			if err != nil {
				return firewall.Result{}, err
			}
		}
		settings, _, _, err := repo.LoadSettingsRevision(t.Context())
		if err != nil {
			return firewall.Result{}, err
		}
		settings.FeatureFlags["enable_apply_beta"], settings.AdvancedAcknowledgedAt = true, time.Now().Unix()
		if err := repo.SaveSettings(t.Context(), settings); err != nil {
			return firewall.Result{}, err
		}
		scope := "server-protection:apply"
		router := gin.New()
		protectionapi.RegisterRoutes(router.Group("/api"), protectionapi.Deps{
			Repository: repo, Operations: workflow.Manager, Firewall: &workflow,
			RequireScope: func(c *gin.Context, _ string, allowed ...string) bool {
				for _, candidate := range allowed {
					if scope == candidate {
						return true
					}
				}
				c.AbortWithStatus(http.StatusForbidden)
				return false
			},
			Actor: func(*gin.Context) string { return "fixture-operator" },
			Audit: func(*gin.Context, string, string, string, string, map[string]any) {},
			JSONObj: func(c *gin.Context, value interface{}, err error) {
				c.JSON(http.StatusOK, gin.H{"success": err == nil, "obj": value})
			},
		})
		request := func(method, path, body string) *httptest.ResponseRecorder {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/api/components/server-protection"+path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			return recorder
		}
		status := request(http.MethodGet, "/operations", "")
		var projected struct {
			Obj struct {
				Items []struct {
					OperationID       string `json:"operationId"`
					RollbackAvailable bool   `json:"rollbackAvailable"`
					DecisionRequired  bool   `json:"operatorDecisionRequired"`
					Reason            string `json:"operatorReason"`
				} `json:"items"`
			} `json:"obj"`
		}
		if status.Code != http.StatusOK || json.Unmarshal(status.Body.Bytes(), &projected) != nil {
			return firewall.Result{}, fmt.Errorf("public status unavailable")
		}
		found := false
		for _, item := range projected.Obj.Items {
			if item.OperationID == id {
				found = item.RollbackAvailable && (calls != 1 || item.DecisionRequired && item.Reason == "firewall_restore_expired")
			}
		}
		if !found {
			return firewall.Result{}, fmt.Errorf("public status did not expose semantic Rollback")
		}
		body, _ := json.Marshal(map[string]string{"operationId": id, "confirmation": "ROLLBACK SERVER PROTECTION " + id})
		for _, deniedScope := range []string{"", "server-protection:read", "server-protection:write"} {
			scope = deniedScope
			if response := request(http.MethodPost, "/firewall/rollback", string(body)); response.Code != http.StatusForbidden {
				return firewall.Result{}, fmt.Errorf("public scope bypass")
			}
		}
		scope = "server-protection:apply"
		if response := request(http.MethodPost, "/firewall/rollback", `{"operationId":"`+id+`","confirmation":"wrong"}`); response.Code != http.StatusBadRequest {
			return firewall.Result{}, fmt.Errorf("public confirmation bypass")
		}
		response := request(http.MethodPost, "/firewall/rollback", string(body))
		var envelope struct {
			Success bool            `json:"success"`
			Obj     firewall.Result `json:"obj"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &envelope) != nil || !envelope.Success || envelope.Obj.OperationID != id || envelope.Obj.State != operations.StateRolledBack {
			return firewall.Result{}, fmt.Errorf("normal public Rollback failed: status=%d body=%s", response.Code, response.Body.String())
		}
		t.Logf("public Rollback %d preserved original operation and completed", calls)
		return envelope.Obj, nil
	})
	if calls != 2 {
		t.Fatal("both retained and post-restore public Rollback must execute exactly once")
	}
}
