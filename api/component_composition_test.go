package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MalenkiySolovey/solovey-host/operationref"
	"github.com/MalenkiySolovey/solovey-ui/backend/actions"
	"github.com/MalenkiySolovey/solovey-ui/backend/componentroutes"
	"github.com/MalenkiySolovey/solovey-ui/backend/componentview"
	"github.com/MalenkiySolovey/solovey-ui/backend/hostcontext"
	"github.com/gin-gonic/gin"
)

type componentInputFixture struct {
	componentroutes.Composition
	active       bool
	stateErr     error
	declared     bool
	activeReads  int
	ownerEffects int
	declarations int
}

func (f *componentInputFixture) Routes(componentroutes.Ports) ([]componentroutes.Route, error) {
	if f == nil {
		return nil, componentroutes.ErrUnavailable
	}
	return []componentroutes.Route{{ComponentID: "panel-update-ui", Register: func(g *gin.RouterGroup) error {
		g.POST("/composition-proof", func(c *gin.Context) {
			if c.GetString(componentAuthorityKey) != "panel-update-ui" {
				panic("missing server-bound Component authority")
			}
			f.ownerEffects++
			c.Status(http.StatusNoContent)
		})
		return nil
	}}}, nil
}
func (f *componentInputFixture) IsActive(string) (bool, error) {
	if f == nil {
		return false, componentroutes.ErrUnavailable
	}
	f.activeReads++
	return f.active, f.stateErr
}
func (f *componentInputFixture) DeclaresAction(_, _ string) bool {
	if f == nil {
		return false
	}
	f.declarations++
	return f.declared
}
func (f *componentInputFixture) Inventory() (componentview.RuntimeInventory, error) {
	return componentview.RuntimeInventory{}, componentroutes.ErrUnavailable
}
func (f *componentInputFixture) Owners() ([]componentroutes.OwnerAvailability, error) {
	return nil, componentroutes.ErrUnavailable
}

func TestComponentRouteInputPreservesActiveGateAndServerAuthority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name                   string
		host                   []string
		active                 bool
		stateErr               error
		status, reads, effects int
	}{
		{name: "local", active: true, status: http.StatusNoContent, reads: 1, effects: 1},
		{name: "exact local", host: []string{"local"}, active: true, status: http.StatusNoContent, reads: 1, effects: 1},
		{name: "foreign", host: []string{"remote"}, active: true, status: http.StatusNotFound},
		{name: "empty", host: []string{""}, active: true, status: http.StatusNotFound},
		{name: "duplicate", host: []string{"local", "local"}, active: true, status: http.StatusNotFound},
		{name: "mixed", host: []string{"local,remote"}, active: true, status: http.StatusNotFound},
		{name: "disabled", status: http.StatusConflict, reads: 1},
		{name: "state unavailable", active: true, stateErr: errors.New("owner read failed"), status: http.StatusInternalServerError, reads: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			owner := &componentInputFixture{active: test.active, stateErr: test.stateErr}
			api := ApiService{Components: owner}
			router := gin.New()
			group := router.Group("/api")
			group.Use(hostcontext.Middleware())
			api.registerComponentAPIRoutes(group, componentroutes.Ports{})
			request := httptest.NewRequest(http.MethodPost, "/api/composition-proof", nil)
			request.Header.Set("X-Component-ID", "foreign-component")
			for _, value := range test.host {
				request.Header.Add(hostcontext.Header, value)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.status || owner.activeReads != test.reads || owner.ownerEffects != test.effects {
				t.Fatalf("status=%d reads=%d effects=%d; body=%s", response.Code, owner.activeReads, owner.ownerEffects, response.Body)
			}
		})
	}
}

func TestComponentActionInputDeniesMissingDeclarationBeforeStepUp(t *testing.T) {
	initSessionTestDB(t)
	invocation := actions.RemoveComponent(operationref.NewReference("component-lifecycle", "component:telegram", "component-remove:composition-proof"), actions.ComponentRemoveInput{ComponentID: "telegram"})
	var typedNil *componentInputFixture
	for _, test := range []struct {
		name                string
		owner               componentroutes.Composition
		component           string
		declared, cancelled bool
	}{
		{name: "missing", component: "panel-update-ui"},
		{name: "typed nil", owner: typedNil, component: "panel-update-ui"},
		{name: "undeclared", owner: &componentInputFixture{}, component: "panel-update-ui"},
		{name: "missing authority", owner: &componentInputFixture{declared: true}},
		{name: "foreign authority", owner: &componentInputFixture{declared: true}, component: "telegram"},
		{name: "cancelled", owner: &componentInputFixture{declared: true}, component: "panel-update-ui", cancelled: true},
		{name: "declared", owner: &componentInputFixture{declared: true}, component: "panel-update-ui", declared: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			api := NewApiService(WithComponents(test.owner))
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/update/components/telegram/remove", nil)
			c.Set(apiUsernameKey, "administrator")
			c.Set(apiTokenScopeKey, "update")
			c.Set(componentAuthorityKey, test.component)
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			if test.cancelled {
				cancel()
			}
			assurance := 0
			_, err := actions.Require(ctx, invocation, api.actionPolicy(func(*gin.Context, string, string) bool { assurance++; return true })(c))
			if test.declared {
				if err != nil || assurance != 1 {
					t.Fatalf("declared action rejected: %v calls=%d", err, assurance)
				}
			} else if !errors.Is(err, actions.ErrDenied) || assurance != 0 {
				t.Fatalf("unbound action reached assurance: %v calls=%d", err, assurance)
			}
		})
	}
}

func TestComponentInventoryAndOwnersFailClosedWithoutComposition(t *testing.T) {
	var typedNil *componentInputFixture
	for _, owner := range []componentroutes.Composition{nil, typedNil} {
		handler := &APIHandler{ApiService: NewApiService(WithComponents(owner))}
		if _, err := componentroutes.Inventory(handler.Components); !errors.Is(err, componentroutes.ErrUnavailable) {
			t.Fatal("missing inventory accepted", err)
		}
		statuses, reasons := handler.installedOwnerStatus()
		if len(statuses) != 1 || len(reasons) != 1 || reasons[0] != "installed_owner_manifest_unavailable" || !ownerBackupBlocked(reasons) {
			t.Fatal("missing owner input allowed backup", statuses, reasons)
		}
		router := gin.New()
		handler.registerComponentStatusRoutes(router.Group("/api"))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/components", nil))
		if response.Code != http.StatusOK || !containsResponseFailure(response.Body.String()) {
			t.Fatal("missing composition exposed inventory", response.Code, response.Body.String())
		}
	}
}

func containsResponseFailure(body string) bool {
	var response struct{ Success bool }
	return json.Unmarshal([]byte(body), &response) == nil && !response.Success
}
