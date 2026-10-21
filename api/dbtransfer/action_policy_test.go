package dbtransfer

import (
	"context"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fixtureActionPolicy(*gin.Context) actions.Authorizer {
	return func(_ context.Context, actual actions.Invocation) (actions.Decision, error) {
		return actions.Decision{Allowed: true, Actor: "fixture-admin", Permission: actions.PermissionBackupRestore, Invocation: actual}, nil
	}
}

func TestDatabaseImportFailsClosedWithoutSemanticActionPolicy(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/importdb", nil)
	h := NewHandler(Deps{RequireScope: func(*gin.Context, string, ...string) bool { return true },
		RequireStepUp: func(*gin.Context, string, string) bool { return true }, AuthorizationChanged: func() {}})
	h.ImportDb(c)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing owner policy admitted: %d", recorder.Code)
	}
}
