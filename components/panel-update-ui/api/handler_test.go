package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	panelupdateservice "github.com/MalenkiySolovey/solovey-ui/components/panel-update-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/gin-gonic/gin"
)

type componentManagerStepUpStub struct {
	removeCalls int
}

func (stub *componentManagerStepUpStub) Enable(panelupdateservice.OperationContext, string) (panelupdateservice.ComponentStatus, error) {
	return panelupdateservice.ComponentStatus{}, nil
}

func (stub *componentManagerStepUpStub) Disable(panelupdateservice.OperationContext, string) (panelupdateservice.ComponentStatus, error) {
	return panelupdateservice.ComponentStatus{}, nil
}

func (stub *componentManagerStepUpStub) Install(panelupdateservice.OperationContext, string) (panelupdateservice.ComponentStatus, error) {
	return panelupdateservice.ComponentStatus{}, nil
}

func (stub *componentManagerStepUpStub) Remove(_ panelupdateservice.OperationContext, id string) (panelupdateservice.ComponentStatus, error) {
	stub.removeCalls++
	return panelupdateservice.ComponentStatus{ID: id, Installed: false}, nil
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

type componentCatalogStub struct {
	inventory panelupdateservice.Inventory
}

func (stub componentCatalogStub) Inventory() (panelupdateservice.Inventory, error) {
	return stub.inventory, nil
}

func TestComponentsEndpointReturnsUpdateCatalogInventory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router.Group("/api"), Deps{
		Components: componentCatalogStub{
			inventory: panelupdateservice.Inventory{
				BinaryProfile: "full",
				Installed: []panelupdateservice.ComponentStatus{
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
						Group:             panelupdateservice.GroupInstalled,
					},
				},
				Available: []panelupdateservice.ComponentStatus{
					{
						ID:                "paid-subscriptions",
						Name:              "Paid Subscriptions",
						Version:           "1",
						Delivery:          manifest.DeliveryInProcess,
						AvailableInBinary: true,
						Installable:       true,
						Group:             panelupdateservice.GroupAvailable,
					},
				},
				Unavailable: []panelupdateservice.ComponentStatus{
					{
						ID:                "future-component",
						Name:              "Future Component",
						Version:           "1",
						Delivery:          manifest.DeliveryInProcess,
						AvailableInBinary: false,
						Installable:       false,
						Group:             panelupdateservice.GroupUnavailable,
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
			Installed   []panelupdateservice.ComponentStatus `json:"installed"`
			Available   []panelupdateservice.ComponentStatus `json:"available"`
			Unavailable []panelupdateservice.ComponentStatus `json:"unavailable"`
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
