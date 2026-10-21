//go:build !minimal

package api

import (
	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestActionAdapterUsesActualComponentDeclarationAndKnownPermissionScope(t *testing.T) {
	initSessionTestDB(t)
	api := NewApiService()
	actual := actions.RemoveComponent(operationcoordination.NewReference("component-lifecycle", "component:telegram", "component-remove:policy-proof"), actions.ComponentRemoveInput{ComponentID: "telegram"})
	for _, variant := range []string{"allowed", "missing_component", "foreign_component", "unknown_action", "wrong_scope"} {
		t.Run(variant, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/update/components/telegram/remove", nil)
			c.Request.Header.Set("X-Component-ID", "panel-update-ui") // A client header grants no authority.
			c.Set(apiUsernameKey, "test-admin")
			c.Set(apiTokenScopeKey, "update")
			c.Set(componentAuthorityKey, "panel-update-ui")
			invocation := actual
			switch variant {
			case "missing_component":
				c.Set(componentAuthorityKey, "")
			case "foreign_component":
				c.Set(componentAuthorityKey, "telegram")
			case "unknown_action":
				invocation.ID = "future.execute"
			case "wrong_scope":
				c.Set(apiTokenScopeKey, "read")
			}
			assured := false
			policy := api.actionPolicy(func(_ *gin.Context, action, target string) bool {
				assured = true
				if action != actions.ComponentRemove || target != "component:telegram" {
					t.Fatal("wrong action target reached assurance boundary")
				}
				return true
			})(c)
			_, err := actions.Require(c.Request.Context(), invocation, policy)
			if variant == "allowed" {
				if err != nil || !assured {
					t.Fatalf("declared scoped component action rejected: %v", err)
				}
			} else if err == nil || assured {
				t.Fatalf("invalid component authority reached step-up: %v %v", err, assured)
			}
		})
	}
}
