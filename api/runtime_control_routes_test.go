package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func TestRuntimeRoutesUseCurrentBearerScopesAndAudit(t *testing.T) {
	initSessionTestDB(t)
	completeTokenOwnerResetForTest(t)
	core := coreruntime.NewCore()
	if err := core.Start([]byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Stop() })
	runtime := service.NewRuntime(core)
	userService := &service.UserService{Runtime: runtime}
	tokens := map[string]string{}
	for _, scope := range []string{"admin", "read", "write", "observability", "database", "update"} {
		plain, err := userService.AddToken("admin", 0, "runtime fixture", scope)
		if err != nil {
			t.Fatal(err)
		}
		tokens[scope] = plain
	}
	router := gin.New()
	h := NewAPIv2Handler(router.Group("/apiv2"), WithRuntime(runtime))
	request := func(method, route, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/apiv2/runtime/"+route, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	generation := core.RuntimeStatus(t.Context()).Generation
	body := `{"generation":"` + generation + `","clientId":7}`
	for scope, token := range tokens {
		status := request(http.MethodGet, "status", token, "")
		wantStatus := http.StatusOK
		if scope == "database" || scope == "update" {
			wantStatus = http.StatusForbidden
		}
		if status.Code != wantStatus {
			t.Fatalf("%s status route code=%d", scope, status.Code)
		}
		list := request(http.MethodGet, "sessions?clientId=7", token, "")
		wantRead := scope == "admin" || scope == "read" || scope == "write"
		if (list.Code == http.StatusOK) != wantRead {
			t.Fatalf("%s identity listing policy=%d", scope, list.Code)
		}
		closeResponse := request(http.MethodPost, "disconnect", token, body)
		wantWrite := scope == "admin" || scope == "write"
		if (closeResponse.Code == http.StatusOK) != wantWrite {
			t.Fatalf("%s disconnect policy=%d", scope, closeResponse.Code)
		}
		groupBody := `{"generation":"` + generation + `","group":"missing","member":"direct"}`
		if (request(http.MethodGet, "groups", token, "").Code == http.StatusOK) != wantRead {
			t.Fatalf("%s runtime group read policy", scope)
		}
		for _, action := range []string{"select", "probe"} {
			if (request(http.MethodPost, action, token, groupBody).Code == http.StatusOK) != wantWrite {
				t.Fatalf("%s %s policy", scope, action)
			}
		}
		maintenance := request(http.MethodPost, "maintenance", token, `{"generation":"00000000-0000-4000-8000-000000000001","enabled":true}`)
		if (maintenance.Code == http.StatusOK) != (scope == "admin") {
			t.Fatalf("%s maintenance policy=%d", scope, maintenance.Code)
		}
		log := request(http.MethodGet, "logs?generation=invalid", token, "")
		wantLogs := wantRead || scope == "observability"
		if (log.Code == http.StatusBadRequest) != wantLogs {
			t.Fatalf("%s log read policy=%d", scope, log.Code)
		}
		if wantWrite && !strings.Contains(closeResponse.Body.String(), "ALREADY_GONE") {
			t.Fatal("zero flow result not explicit")
		}
	}
	flushAPIAudit(t)
	var count int64
	if err := dbsqlite.DB().Model(&model.AuditEvent{}).Where("event = ? AND actor = ?", "runtime_disconnect", "admin").Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("attributable audit count=%d err=%v", count, err)
	}
	if err := dbsqlite.DB().Model(&model.Tokens{}).Where("scope = ?", "write").Update("scope", "read").Error; err != nil {
		t.Fatal(err)
	}
	if request(http.MethodPost, "disconnect", tokens["write"], body).Code != http.StatusForbidden {
		t.Fatal("cached token retained revoked mutation authority")
	}
	h.ReloadTokens()
	if request(http.MethodGet, "sessions", "invalid", "").Code != http.StatusUnauthorized {
		t.Fatal("invalid token read runtime")
	}
	if request(http.MethodGet, "sessions?limit=257", tokens["admin"], "").Code != http.StatusBadRequest {
		t.Fatal("unbounded list")
	}
	if request(http.MethodPost, "disconnect", tokens["admin"], `{"generation":"secret-as-target","clientId":7}`).Code != http.StatusBadRequest {
		t.Fatal("arbitrary payload accepted as audit identity")
	}
	for action, valid := range map[string]string{
		"disconnect":  body,
		"select":      `{"generation":"` + generation + `","group":"missing","member":"direct"}`,
		"probe":       `{"generation":"` + generation + `","group":"missing","member":"direct"}`,
		"maintenance": `{"generation":"` + generation + `","enabled":true}`,
	} {
		for _, invalid := range []string{valid + `{}`, valid[:len(valid)-1] + `,"unknown":true}`, strings.Repeat(" ", 2048) + valid} {
			if request(http.MethodPost, action, tokens["admin"], invalid).Code != http.StatusBadRequest {
				t.Fatalf("%s accepted trailing/unknown/oversize input", action)
			}
		}
	}
	stale := request(http.MethodPost, "disconnect", tokens["admin"], `{"generation":"00000000-0000-4000-8000-000000000001","clientId":7}`)
	if !strings.Contains(stale.Body.String(), "STALE_GENERATION") {
		t.Fatal("late runtime action not fenced")
	}
	read := request(http.MethodGet, "sessions", tokens["read"], "")
	var response struct {
		Obj runtimeSessionsView `json:"obj"`
	}
	if json.Unmarshal(read.Body.Bytes(), &response) != nil || response.Obj.Capabilities.FlowClose || response.Obj.Snapshot == nil || response.Obj.Snapshot.Total != 0 {
		t.Fatal("read authority or empty state misrepresented")
	}
}
