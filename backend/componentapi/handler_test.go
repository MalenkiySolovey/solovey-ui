package componentapi

import (
	"context"
	"encoding/json"
	operationcoordination "github.com/MalenkiySolovey/solovey-host/operationref"
	"github.com/MalenkiySolovey/solovey-ui/backend/actions"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-host/componenthost/manifest"
	"github.com/MalenkiySolovey/solovey-ui/backend/componentview"
	"github.com/gin-gonic/gin"
)

type componentManagerStepUpStub struct {
	removeCalls int
	operation   componentview.OperationContext
}

func (stub *componentManagerStepUpStub) Enable(componentview.OperationContext, string) (componentview.ComponentStatus, error) {
	return componentview.ComponentStatus{}, nil
}

func (stub *componentManagerStepUpStub) Disable(componentview.OperationContext, string) (componentview.ComponentStatus, error) {
	return componentview.ComponentStatus{}, nil
}

func (stub *componentManagerStepUpStub) Install(componentview.OperationContext, string) (componentview.ComponentStatus, error) {
	return componentview.ComponentStatus{}, nil
}

func (stub *componentManagerStepUpStub) Remove(operation componentview.OperationContext, id string) (componentview.ComponentStatus, error) {
	invocation := actions.RemoveComponent(operationcoordination.NewReference("component-lifecycle", "component:"+id, "component-remove:fixture"), actions.ComponentRemoveInput{ComponentID: id})
	if _, err := actions.Require(operation.RequestContext, invocation, operation.Authorize); err != nil {
		return componentview.ComponentStatus{}, err
	}
	stub.removeCalls++
	stub.operation = operation
	return componentview.ComponentStatus{ID: id, Installed: false}, nil
}

func TestComponentRemoveFailsClosedWithoutReauthenticationBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager := &componentManagerStepUpStub{}
	router := gin.New()
	RegisterRoutes(router.Group("/api"), Deps{
		ComponentManager: manager,
		RequireScope:     func(*gin.Context, string, ...string) bool { return true },
		LoginUser:        func(*gin.Context) string { return "admin" },
		RemoteIP:         func(*gin.Context) string { return "192.0.2.1" },
		Audit:            func(*gin.Context, string, string, string, string, map[string]any) {},
		JSONMsg: func(c *gin.Context, msg string, err error) {
			c.JSON(http.StatusOK, gin.H{"success": err == nil, "msg": msg})
		},
		JSONObj: func(c *gin.Context, obj any, err error) {
			c.JSON(http.StatusOK, gin.H{"success": err == nil, "obj": obj})
		},
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/update/components/telegram/remove",
		strings.NewReader(`{"password":"credential"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable || manager.removeCalls != 0 {
		t.Fatalf("remove response=%d calls=%d body=%s", recorder.Code, manager.removeCalls, recorder.Body.String())
	}
}

func TestComponentRemoveRequiresExactActionTargetAtOwner(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied", true: "accepted"}[allowed], func(t *testing.T) {
			manager := &componentManagerStepUpStub{}
			router := gin.New()
			requireStepUp := func(c *gin.Context, action, target string) bool {
				if action != actions.ComponentRemove || target != "component:telegram" {
					t.Fatalf("wrong assurance binding: %q %q", action, target)
				}
				if !allowed {
					c.AbortWithStatus(http.StatusForbidden)
				}
				return allowed
			}
			RegisterRoutes(router.Group("/api"), Deps{
				ComponentManager: manager,
				RequireScope:     func(*gin.Context, string, ...string) bool { return true },
				LoginUser:        func(*gin.Context) string { return "admin" }, RemoteIP: func(*gin.Context) string { return "192.0.2.1" },
				Audit: func(*gin.Context, string, string, string, string, map[string]any) {},
				JSONObj: func(c *gin.Context, obj any, err error) {
					c.JSON(http.StatusOK, gin.H{"success": err == nil, "obj": obj})
				},
				JSONMsg: func(c *gin.Context, msg string, err error) {
					c.JSON(http.StatusOK, gin.H{"success": err == nil, "msg": msg})
				},
				RequireStepUp: requireStepUp,
				ActionPolicy: func(c *gin.Context) actions.Authorizer {
					return func(_ context.Context, actual actions.Invocation) (actions.Decision, error) {
						if !requireStepUp(c, actual.ID, actual.Target) {
							return actions.Decision{}, actions.ErrDenied
						}
						return actions.Decision{Allowed: true, Actor: "admin", Permission: actions.PermissionComponentUninstall, Invocation: actual}, nil
					}
				},
			})
			request := httptest.NewRequest(http.MethodPost, "/api/update/components/telegram/remove", strings.NewReader(`{}`))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if allowed && (manager.removeCalls != 1 || manager.operation.RequestContext != request.Context() || recorder.Code != http.StatusOK) {
				t.Fatal("accepted owner action lost its request context")
			}
			if !allowed && (manager.removeCalls != 0 || recorder.Code != http.StatusForbidden) {
				t.Fatal("rejected assurance reached mutation")
			}
		})
	}
}

type componentCatalogStub struct {
	inventory componentview.Inventory
}

func (stub componentCatalogStub) Inventory() (componentview.Inventory, error) {
	return stub.inventory, nil
}

func TestComponentsEndpointReturnsUpdateCatalogInventory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router.Group("/api"), Deps{
		Components: componentCatalogStub{
			inventory: componentview.Inventory{
				BinaryProfile: "full",
				Installed: []componentview.ComponentStatus{
					{
						ID:                "telegram",
						Name:              "Telegram",
						Version:           "1",
						Delivery:          manifest.DeliveryInProcess,
						AvailableInBinary: true,
						Installable:       true,
						Installed:         true,
						Enabled:           true,
						Active:            true,
						Group:             componentview.GroupInstalled,
					},
				},
				Available: []componentview.ComponentStatus{
					{
						ID:                "paid-subscriptions",
						Name:              "Paid Subscriptions",
						Version:           "1",
						Delivery:          manifest.DeliveryInProcess,
						AvailableInBinary: true,
						Installable:       true,
						Group:             componentview.GroupAvailable,
					},
				},
				Unavailable: []componentview.ComponentStatus{
					{
						ID:                "future-component",
						Name:              "Future Component",
						Version:           "1",
						Delivery:          manifest.DeliveryInProcess,
						AvailableInBinary: false,
						Installable:       false,
						Group:             componentview.GroupUnavailable,
						UnavailableReason: "not bundled in this binary profile",
					},
				},
			},
		},
		RequireScope: func(*gin.Context, string, ...string) bool { return true },
		JSONObj: func(context *gin.Context, value any, err error) {
			if err != nil {
				context.JSON(http.StatusOK, gin.H{"success": false, "msg": err.Error()})
				return
			}
			context.JSON(http.StatusOK, gin.H{"success": true, "obj": value})
		},
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/update/components", nil))

	var response struct {
		Success bool `json:"success"`
		Obj     struct {
			Installed   []componentview.ComponentStatus `json:"installed"`
			Available   []componentview.ComponentStatus `json:"available"`
			Unavailable []componentview.ComponentStatus `json:"unavailable"`
		} `json:"obj"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success {
		t.Fatalf("response must be successful: %s", recorder.Body.String())
	}
	if len(response.Obj.Installed) != 1 || len(response.Obj.Available) != 1 || len(response.Obj.Unavailable) != 1 {
		t.Fatalf("catalog groups were not exposed by update component: %#v", response.Obj)
	}
	if response.Obj.Unavailable[0].ID != "future-component" || response.Obj.Unavailable[0].UnavailableReason == "" {
		t.Fatalf("unavailable component metadata was not exposed: %#v", response.Obj.Unavailable)
	}
}

func TestComponentsEndpointFailsClosedWithoutScopeBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router.Group("/api"), Deps{
		Components: componentCatalogStub{},
		JSONObj: func(context *gin.Context, value any, err error) {
			context.JSON(http.StatusOK, gin.H{"success": err == nil, "obj": value})
		},
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/update/components", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("response=%d body=%s, want fail-closed 503", recorder.Code, recorder.Body.String())
	}
}
