//go:build shtorm_extended && with_quic

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"github.com/gin-gonic/gin"
)

func TestNativeCredentialHTTPAuthorizationMaskingAndStaleDraft(t *testing.T) {
	for _, kind := range []string{"mieru", "trusttunnel"} {
		t.Run(kind, func(t *testing.T) {
			settings := initSessionTestDB(t)
			if _, err := settings.GetAllSetting(); err != nil {
				t.Fatal(err)
			}
			if err := settings.Save(dbsqlite.DB(), json.RawMessage(`{"webPath":"/"}`)); err != nil {
				t.Fatal(err)
			}
			completeTokenOwnerResetForTest(t)
			prepareComponentRouteMetadata(t)
			runtime := service.NewRuntime(coreruntime.NewCore())
			if err := runtime.Core().StartWithBindings([]byte(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`), nil); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := runtime.Core().Stop(); err != nil {
					t.Error(err)
				}
				closeAPITestRuntime(t, runtime)
			})
			tokens := map[string]string{}
			for _, scope := range []string{"read", "write", "observability"} {
				value, err := (&service.UserService{Runtime: runtime}).AddToken("admin", 0, "native credential fixture", scope)
				if err != nil {
					t.Fatal(err)
				}
				tokens[scope] = value
			}
			router, cookies := newAuthenticatedTestRouter(t, settings, func(router *gin.Engine) {
				v2 := NewAPIv2Handler(router.Group("/apiv2"), WithRuntime(runtime))
				NewAPIHandler(router.Group("/api"), v2, WithRuntime(runtime))
			})
			const firstSecret, secondSecret = "synthetic-native-api-first", "synthetic-native-api-second"
			var captured string
			checkRedacted := func(raw []byte) {
				t.Helper()
				for _, value := range []string{firstSecret, secondSecret, captured, "sbox:v1:"} {
					if value != "" && bytes.Contains(raw, []byte(value)) {
						t.Fatal("ordinary HTTP/audit/change projection exposed a native credential")
					}
				}
			}
			request := func(method, target, token string, form url.Values) *httptest.ResponseRecorder {
				t.Helper()
				req := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
				if method == http.MethodPost {
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				}
				if token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				checkRedacted(response.Body.Bytes())
				return response
			}
			client := model.Client{Name: "native-" + kind, Enable: true, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`)}
			mutation := func(value any) {
				t.Helper()
				client.Config, _ = json.Marshal(map[string]any{kind: map[string]any{"name": client.Name, "password": value}})
			}
			unchanged := func(view secretref.View) map[string]any {
				return map[string]any{"intent": "unchanged", "reference": view.Reference, "present": view.Present, "rotationRevision": view.RotationRevision}
			}
			saveForm := func(action string) url.Values {
				t.Helper()
				data, err := json.Marshal(client)
				if err != nil {
					t.Fatal(err)
				}
				return url.Values{"object": {"clients"}, "action": {action}, "data": {string(data)}}
			}
			assertSuccess := func(response *httptest.ResponseRecorder, want bool) {
				t.Helper()
				var envelope Msg
				if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &envelope) != nil || envelope.Success != want {
					t.Fatalf("native credential HTTP outcome: status=%d success=%t want=%t", response.Code, envelope.Success, want)
				}
			}
			stored := func() model.Client {
				t.Helper()
				var value model.Client
				if err := dbsqlite.DB().Where("name = ?", client.Name).First(&value).Error; err != nil {
					t.Fatal(err)
				}
				return value
			}
			password := func(value model.Client) string {
				t.Helper()
				var root map[string]struct{ Password string }
				if json.Unmarshal(value.Config, &root) != nil {
					t.Fatal("stored native credential has invalid shape")
				}
				return root[kind].Password
			}
			read := func() (model.Client, secretref.View) {
				t.Helper()
				target := "/apiv2/clients?id=" + strconv.Itoa(int(client.Id))
				response := request(http.MethodGet, target, tokens["read"], nil)
				assertSuccess(response, true)
				var envelope struct {
					Obj struct{ Clients []model.Client }
				}
				if json.Unmarshal(response.Body.Bytes(), &envelope) != nil || len(envelope.Obj.Clients) != 1 {
					t.Fatal("normal HTTP read omitted the native user")
				}
				var root map[string]struct{ Password secretref.View }
				if json.Unmarshal(envelope.Obj.Clients[0].Config, &root) != nil || root[kind].Password.Reference.Owner != "users" {
					t.Fatal("normal HTTP read omitted the Users-owned secret reference")
				}
				return envelope.Obj.Clients[0], root[kind].Password
			}
			mutation(map[string]any{"intent": "replace", "value": firstSecret})
			if got := request(http.MethodPost, "/apiv2/save", tokens["read"], saveForm("new")); got.Code != http.StatusForbidden {
				t.Fatal("read token acquired native credential mutation authority")
			}
			assertSuccess(request(http.MethodPost, "/apiv2/save", tokens["write"], saveForm("new")), true)
			client = stored()
			captured = password(client)
			if !secretbox.IsEncrypted(captured) || bytes.Contains(client.Config, []byte(firstSecret)) {
				t.Fatal("HTTP owner save did not seal the native password")
			}
			projected, original := read()
			if !original.Present || original.RotationRevision == "" {
				t.Fatal("normal HTTP read lost credential presence/revision")
			}
			for token, status := range map[string]int{"": http.StatusUnauthorized, tokens["observability"]: http.StatusForbidden} {
				if response := request(http.MethodGet, "/apiv2/clients?id="+strconv.Itoa(int(client.Id)), token, nil); response.Code != status {
					t.Fatal("native credential read bypassed existing authorization")
				}
			}
			browser := performAuthenticatedTestRequest(router, httptest.NewRequest(http.MethodGet, "/api/clients?id="+strconv.Itoa(int(client.Id)), nil), cookies...)
			checkRedacted(browser.Body.Bytes())
			assertSuccess(browser, true)
			if !bytes.Contains(browser.Body.Bytes(), []byte(original.RotationRevision)) {
				t.Fatal("browser and bearer reads diverged from the same credential owner")
			}
			client = projected
			client.Desc = "metadata through shared owner"
			mutation(unchanged(original))
			assertSuccess(request(http.MethodPost, "/apiv2/save", tokens["write"], saveForm("edit")), true)
			if password(stored()) != captured {
				t.Fatal("HTTP metadata edit rotated an unchanged credential")
			}
			mutation(map[string]any{"intent": "replace", "value": secondSecret})
			assertSuccess(request(http.MethodPost, "/apiv2/save", tokens["write"], saveForm("edit")), true)
			rotated := stored()
			if password(rotated) == captured {
				t.Fatal("explicit HTTP replacement retained the old credential")
			}
			captured = password(rotated)
			_, current := read()
			if current.RotationRevision == original.RotationRevision {
				t.Fatal("HTTP replacement did not change the revision")
			}
			client.Desc = "stale edit must not commit"
			mutation(unchanged(original))
			assertSuccess(request(http.MethodPost, "/apiv2/save", tokens["write"], saveForm("edit")), false)
			if after := stored(); password(after) != captured || after.Desc != rotated.Desc {
				t.Fatal("stale HTTP draft partially committed credential or metadata")
			}
			mutation(map[string]any{"intent": "clear"})
			if response := request(http.MethodPost, "/apiv2/save", tokens["read"], saveForm("edit")); response.Code != http.StatusForbidden || password(stored()) != captured {
				t.Fatal("read token cleared the native credential")
			}
			client.Desc = rotated.Desc
			assertSuccess(request(http.MethodPost, "/apiv2/save", tokens["write"], saveForm("edit")), true)
			_, cleared := read()
			if cleared.Present || password(stored()) != "" {
				t.Fatal("explicit HTTP clear did not preserve an absent reference")
			}
			stopAPITestRuntimeAudit(t, runtime)
			var audits []model.AuditEvent
			var changes []model.Changes
			if err := dbsqlite.DB().Find(&audits).Error; err != nil {
				t.Fatal(err)
			}
			if err := dbsqlite.DB().Find(&changes).Error; err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal([]any{audits, changes})
			if err != nil {
				t.Fatal(err)
			}
			checkRedacted(encoded)
		})
	}
}
