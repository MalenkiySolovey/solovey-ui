package bearer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	panelsecurity "github.com/MalenkiySolovey/solovey-ui/backend/security"
	"github.com/gin-gonic/gin"
)

type admissionCommands struct {
	authorize func(context.Context, uint, string, int64) (panelsecurity.APITokenAuthorization, error)
	recorded  atomic.Int32
}

func (c *admissionCommands) HashAPIToken(value string) (string, error) { return value, nil }
func (c *admissionCommands) AuthorizeAPIToken(ctx context.Context, id uint, hash string, now int64) (panelsecurity.APITokenAuthorization, error) {
	return c.authorize(ctx, id, hash, now)
}
func (c *admissionCommands) RecordTokenUse(uint, string) error { c.recorded.Add(1); return nil }

func admissionFixture(t *testing.T, commands *admissionCommands) (*Handler, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := New(Deps{Users: commands, LoadTokens: func() ([]byte, error) {
		return json.Marshal([]TokenInMemory{{ID: 7, TokenHash: "fixture-digest", Username: "old-account", Scope: "admin", Enabled: true}})
	}, ClientIP: func(*gin.Context) string { return "127.0.0.1" }, Audit: func(*gin.Context, string, string, string, string, map[string]any) {}})
	h.ReloadTokens()
	router := gin.New()
	router.GET("/principal", h.CheckToken, func(c *gin.Context) { c.String(http.StatusOK, c.GetString(UsernameKey)+":"+c.GetString(ScopeKey)) })
	return h, router
}
func admissionRequest(router *gin.Engine) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/principal", nil)
	request.Header.Set("Authorization", "Bearer fixture-digest")
	result := httptest.NewRecorder()
	router.ServeHTTP(result, request)
	return result
}

func TestBearerAdmissionUsesCurrentDurablePrincipalAndPermission(t *testing.T) {
	commands := &admissionCommands{authorize: func(context.Context, uint, string, int64) (panelsecurity.APITokenAuthorization, error) {
		return panelsecurity.APITokenAuthorization{ID: 7, Username: "current-account", Scope: "read"}, nil
	}}
	_, router := admissionFixture(t, commands)
	result := admissionRequest(router)
	if result.Code != http.StatusOK || result.Body.String() != "current-account:read" {
		t.Fatalf("current principal status=%d body=%q", result.Code, result.Body.String())
	}
	if commands.recorded.Load() != 1 {
		t.Fatal("accepted request use was not recorded once")
	}
}

func TestBearerReloadDuringDurableAuthorizationRejectsPriorDecision(t *testing.T) {
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan *httptest.ResponseRecorder, 1)
	commands := &admissionCommands{authorize: func(context.Context, uint, string, int64) (panelsecurity.APITokenAuthorization, error) {
		close(started)
		<-release
		return panelsecurity.APITokenAuthorization{ID: 7, Username: "old-account", Scope: "admin"}, nil
	}}
	h, router := admissionFixture(t, commands)
	go func() { done <- admissionRequest(router) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("authorization did not reach durable-owner barrier")
	}
	h.ReloadTokens()
	close(release)
	select {
	case result := <-done:
		if result.Code != http.StatusUnauthorized {
			t.Fatalf("old in-flight decision status=%d", result.Code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("authorization did not drain")
	}
	if commands.recorded.Load() != 0 {
		t.Fatal("rejected prior authorization recorded a downstream use")
	}
}
