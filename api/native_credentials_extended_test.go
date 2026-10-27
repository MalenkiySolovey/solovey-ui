//go:build shtorm_extended && with_quic

package api

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
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
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/credentials"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/clientfacts"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/ssh"
)

func TestNativeCredentialHTTPAuthorizationMaskingAndStaleDraft(t *testing.T) {
	for _, kind := range []string{"mieru", "trusttunnel", "ssh", "mtproxy", "sudoku"} {
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
			firstSecret, secondSecret := "synthetic-native-api-first", "synthetic-native-api-second"
			if kind == "mtproxy" {
				var err error
				firstSecret, err = credentials.Generate(kind)
				if err != nil {
					t.Fatal(err)
				}
				secondSecret, err = credentials.Generate(kind)
				if err != nil {
					t.Fatal(err)
				}
			}
			fact, _ := clientfacts.Resolve(kind)
			canaries := []string{firstSecret, secondSecret, "sbox:v1:"}
			if kind == "sudoku" {
				bundle := func() string {
					t.Helper()
					master, err := credentials.GenerateSudokuMaster()
					if err != nil {
						t.Fatal(err)
					}
					split, err := credentials.SplitSudokuMaster(master)
					if err != nil {
						t.Fatal(err)
					}
					canaries = append(canaries, master, split)
					raw, _ := json.Marshal(map[string]string{"7": split})
					return string(raw)
				}
				firstSecret, secondSecret = bundle(), bundle()
				canaries = append(canaries, firstSecret, secondSecret)
			}
			var captured string
			checkRedacted := func(raw []byte) {
				t.Helper()
				for _, value := range append(canaries, captured) {
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
				// Observe each request's durable audit before the next expensive
				// protocol candidate check can occupy the SQLite writer again.
				stopAPITestRuntimeAudit(t, runtime)
				checkRedacted(response.Body.Bytes())
				return response
			}
			client := model.Client{Name: "native-" + kind, Enable: true, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`)}
			mutation := func(value any) {
				t.Helper()
				fields := map[string]any{fact.SecretField: value}
				if !clientfacts.DeliveryOnly(kind) {
					fields["name"] = client.Name
				}
				client.Config, _ = json.Marshal(map[string]any{kind: fields})
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
				var root map[string]map[string]string
				if json.Unmarshal(value.Config, &root) != nil {
					t.Fatal("stored native credential has invalid shape")
				}
				return root[kind][fact.SecretField]
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
				var root map[string]map[string]json.RawMessage
				var view secretref.View
				if json.Unmarshal(envelope.Obj.Clients[0].Config, &root) != nil || json.Unmarshal(root[kind][fact.SecretField], &view) != nil || view.Reference.Owner != "users" {
					t.Fatal("normal HTTP read omitted the Users-owned secret reference")
				}
				return envelope.Obj.Clients[0], view
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
			if kind == "ssh" || kind == "sudoku" {
				secretField, private := "host_key", ""
				inbound := map[string]any{"type": kind, "tag": "http-" + kind + "-owned", "listen": "127.0.0.1", "listen_port": 23456}
				if kind == "ssh" {
					_, key, err := ed25519.GenerateKey(rand.Reader)
					if err != nil {
						t.Fatal(err)
					}
					block, err := ssh.MarshalPrivateKey(key, "runtime-only-api-fixture")
					if err != nil {
						t.Fatal(err)
					}
					private = string(pem.EncodeToMemory(block))
					canaries = append(canaries, strings.Split(private, "\n")[1])
					inbound["server_version"], inbound["max_auth_tries"] = "SSH-2.0-SoloveyFixture", 3
				} else {
					secretField = "master_key"
					var err error
					private, err = credentials.GenerateSudokuMaster()
					if err != nil {
						t.Fatal(err)
					}
					canaries = append(canaries, private)
				}
				inbound[secretField] = map[string]string{"intent": "replace", "value": private}
				form := func(action string) url.Values {
					raw, _ := json.Marshal(inbound)
					return url.Values{"object": {"inbounds"}, "action": {action}, "data": {string(raw)}}
				}
				if response := request(http.MethodPost, "/apiv2/save", tokens["read"], form("new")); response.Code != http.StatusForbidden {
					t.Fatal("read token acquired host-key write authority")
				}
				assertSuccess(request(http.MethodPost, "/apiv2/save", tokens["write"], form("new")), true)
				var row model.Inbound
				if err := dbsqlite.DB().Where("tag = ?", inbound["tag"]).First(&row).Error; err != nil {
					t.Fatal(err)
				}
				sealed := bytes.Clone(row.Options)
				readHost := func() secretref.View {
					response := request(http.MethodGet, "/apiv2/inbounds?id="+strconv.Itoa(int(row.Id)), tokens["read"], nil)
					assertSuccess(response, true)
					var envelope struct {
						Obj struct {
							Inbounds []map[string]json.RawMessage
						}
					}
					if json.Unmarshal(response.Body.Bytes(), &envelope) != nil || len(envelope.Obj.Inbounds) != 1 {
						t.Fatal("host-key HTTP projection missing")
					}
					var view secretref.View
					if json.Unmarshal(envelope.Obj.Inbounds[0][secretField], &view) != nil {
						t.Fatal("inbound private field omitted its secret reference")
					}
					return view
				}
				view := readHost()
				if !view.Present || view.Reference.Owner != "inbounds" || len(view.RotationRevision) != 64 {
					t.Fatal("host-key HTTP read lost its masked reference")
				}
				browser := performAuthenticatedTestRequest(router, httptest.NewRequest(http.MethodGet, "/api/inbounds?id="+strconv.Itoa(int(row.Id)), nil), cookies...)
				checkRedacted(browser.Body.Bytes())
				assertSuccess(browser, true)
				if !bytes.Contains(browser.Body.Bytes(), []byte(view.RotationRevision)) {
					t.Fatal("browser host-key projection diverged")
				}
				inbound["id"], inbound[secretField] = row.Id, unchanged(view)
				assertSuccess(request(http.MethodPost, "/apiv2/save", tokens["write"], form("edit")), true)
				if err := dbsqlite.DB().First(&row, row.Id).Error; err != nil || !bytes.Equal(sealed, row.Options) {
					t.Fatal("HTTP Keep rotated host keys")
				}
				inbound[secretField] = map[string]string{"intent": "replace", "value": private}
				assertSuccess(request(http.MethodPost, "/apiv2/save", tokens["write"], form("edit")), true)
				current := readHost()
				if current.RotationRevision == view.RotationRevision {
					t.Fatal("HTTP host-key rotation retained its revision")
				}
				inbound[secretField] = unchanged(view)
				assertSuccess(request(http.MethodPost, "/apiv2/save", tokens["write"], form("edit")), false)
				if readHost().RotationRevision != current.RotationRevision {
					t.Fatal("stale host-key edit committed")
				}
				inbound[secretField] = map[string]string{"intent": "clear"}
				assertSuccess(request(http.MethodPost, "/apiv2/save", tokens["write"], form("edit")), true)
				if readHost().Present {
					t.Fatal("HTTP Clear retained host keys")
				}
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
