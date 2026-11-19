package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MalenkiySolovey/solovey-backend/actions"
	"github.com/MalenkiySolovey/solovey-host/operationref"
	"github.com/gin-gonic/gin"
)

func TestComponentRemovalPolicyRequiresIntrinsicRouteScopeAndAssurance(t *testing.T) {
	initSessionTestDB(t)
	api := newComposedApiService()
	actual := actions.RemoveComponent(operationref.NewReference("component-lifecycle", "component:telegram", "component-remove:policy-proof"), actions.ComponentRemoveInput{ComponentID: "telegram"})
	for _, variant := range []string{"allowed", "generic_route", "legacy_authority", "foreign_authority", "unknown_action", "wrong_scope", "cancelled"} {
		t.Run(variant, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/update/components/telegram/remove", nil)
			c.Request.Header.Set("X-Component-ID", "panel-update-ui") // A client header grants no authority.
			c.Set(apiUsernameKey, "test-admin")
			c.Set(apiTokenScopeKey, "update")
			intrinsic := true
			invocation := actual
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			switch variant {
			case "generic_route":
				intrinsic = false
			case "legacy_authority":
				c.Set(componentAuthorityKey, "panel-update-ui")
			case "foreign_authority":
				c.Set(componentAuthorityKey, "telegram")
			case "unknown_action":
				invocation.ID = "future.execute"
			case "wrong_scope":
				c.Set(apiTokenScopeKey, "read")
			case "cancelled":
				cancel()
			}
			assured := false
			policy := api.actionPolicyFor(func(_ *gin.Context, action, target string) bool {
				assured = true
				if action != actions.ComponentRemove || target != "component:telegram" {
					t.Fatal("wrong action target reached assurance boundary")
				}
				return true
			}, intrinsic)(c)
			_, err := actions.Require(ctx, invocation, policy)
			if variant == "allowed" {
				if err != nil || !assured {
					t.Fatalf("scoped intrinsic component action rejected: %v", err)
				}
			} else if err == nil || assured {
				t.Fatalf("invalid component authority reached step-up: %v %v", err, assured)
			}
		})
	}
}
