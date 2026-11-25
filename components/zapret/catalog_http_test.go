//go:build !minimal

package zapret

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/gin-gonic/gin"
)

func TestDraftAndPresetRoutesRequireAdminBeforeReadingInput(t *testing.T) {
	for _, tc := range []struct {
		method, path, scope string
	}{
		{http.MethodPost, "/strategies/import/preview", "zapret.strategies.preview"},
		{http.MethodPost, "/strategies/import", "zapret.strategies.write"},
		{http.MethodGet, "/strategies/lua-drafts", "zapret.strategies.read"},
		{http.MethodPost, "/strategies/lua-drafts", "zapret.strategies.write"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			allow := false
			server := gin.New()
			registerRoutes(server.Group("/api"), componenthost.Ports{
				Auth: componenthost.AuthDeps{RequireScope: func(c *gin.Context, scope string, roles ...string) bool {
					if scope != tc.scope || len(roles) != 1 || roles[0] != "admin" {
						t.Fatalf("unexpected authority: %q %v", scope, roles)
					}
					if !allow {
						c.AbortWithStatus(http.StatusForbidden)
					}
					return allow
				}},
				HTTP: componenthost.HTTPDeps{JSONObj: func(c *gin.Context, _ any, err error) {
					if err == nil {
						t.Fatal("invalid input passed request validation")
					}
					c.JSON(http.StatusBadRequest, gin.H{"msg": err.Error()})
				}},
			})
			request := func(body string) *httptest.ResponseRecorder {
				res := httptest.NewRecorder()
				server.ServeHTTP(res, httptest.NewRequest(tc.method, "/api/components/zapret"+tc.path, strings.NewReader(body)))
				return res
			}
			if res := request("invalid"); res.Code != http.StatusForbidden {
				t.Fatalf("unauthorized request received %d: %s", res.Code, res.Body)
			}
			if tc.method == http.MethodGet {
				return // The authorized snapshot is covered by the Backend browser fixture.
			}
			allow = true
			inputs := [][2]string{
				{`{"preset":{"format":"native","content":"AA==","path":"local"}}`, "invalid Zapret request"},
				{`{"preset":{"format":"native","format":"zip","content":"AA=="}}`, "invalid Zapret request"},
				{`{"preset":{"format":"native","content":"AA=="}}`, "missing Zapret catalog revision"},
				{strings.Repeat(" ", maxPresetEnvelope+1), "transfer limit"},
			}
			if tc.path == "/strategies/lua-drafts" {
				inputs = [][2]string{
					{`{"action":"create","draft":{"origin":"external","path":"local"}}`, "invalid Zapret request"},
					{`{"action":"create","draft":{"origin":"administrator"}}`, "missing Lua draft source"},
					{`{"action":"create","draft":{"origin":"administrator","source":""}}`, "missing Zapret catalog revision"},
					{`{"action":"execute"}`, "unknown Lua draft change"},
					{strings.Repeat(" ", 512<<10), "transfer limit"},
				}
			}
			for _, input := range inputs {
				if res := request(input[0]); res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), input[1]) {
					t.Errorf("expected %q, got %d: %s", input[1], res.Code, res.Body)
				}
			}
		})
	}
}
