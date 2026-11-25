//go:build !minimal

package web_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-host/operationref"
	_ "github.com/MalenkiySolovey/solovey-ui/app" // Register the selected Product composition.
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/enabledstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/supervisor"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/web"
)

type suiteFrame struct {
	Kind     string
	Job      operationref.Job
	Progress struct{ GroupsDone, ProbesDone int }
}

func TestBlockCheckProductionStreamPreservesDeadlineAndExactCancellation(t *testing.T) {
	owner, composed := registry.ComponentByID("blockcheck")
	if !composed {
		t.Skip("BlockCheck is not selected in this Product composition")
	}
	settings := owner.Manifest.Database.Settings
	if len(settings) != 1 {
		t.Fatalf("unexpected BlockCheck catalog storage: %v", settings)
	}
	for _, useHTTP2 := range []bool{false, true} {
		t.Run(fmt.Sprintf("http2=%t", useHTTP2), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "installed.json")
			t.Setenv(installstate.InstalledFileEnv, path)
			if _, err := installstate.SetInstalled(path, []manifest.Manifest{owner.Manifest}, owner.Manifest.ID, true); err != nil {
				t.Fatal(err)
			}
			db := web.InitTestRouterDB(t)
			const base = "/qualification+v1/"
			web.SetTestRouterSetting(t, db, "webPath", base)
			web.SetTestRouterSetting(t, db, enabledstate.SettingKey(owner.Manifest.ID), "true")
			web.SetTestRouterSetting(t, db, settings[0], `{"schema":"solovey.blockcheck.groups/v1","groups":[
				{"id":"first","name":"Local fixture","targets":[{"name":"No DNS for literal","host":"9.9.9.9","probes":["dns"]}]},
				{"id":"second","name":"Closing fixture","targets":[{"name":"Synthetic CONNECT target","host":"9.9.9.9","probes":["http"]}]}
			]}`)
			connected := make(chan struct{}, 1)
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// This loopback sink never forwards CONNECT or opens a destination socket.
				if r.Method != http.MethodConnect || r.Host != "9.9.9.9:80" {
					t.Error("the selected proxy did not receive the admitted literal")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				conn, rw, err := http.NewResponseController(w).Hijack()
				if err != nil {
					t.Error("fixture cannot accept CONNECT", err)
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(7 * time.Second))
				_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
				if err := rw.Flush(); err != nil {
					return
				}
				if head, err := http.ReadRequest(rw.Reader); err != nil || head.Method != http.MethodHead || head.Host != "9.9.9.9" {
					t.Error("fixture did not receive the HTTP probe", err)
					return
				}
				connected <- struct{}{}
				_, _ = rw.ReadByte() // Return only after cancellation or the fixture deadline.
			}))
			t.Cleanup(proxy.Close)
			core := coreruntime.NewCore()
			if err := core.Start(fmt.Appendf(nil, `{"outbounds":[{"type":"http","tag":"fixture","server":"127.0.0.1","server_port":%d}]}`, proxy.Listener.Addr().(*net.TCPAddr).Port)); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = core.Stop() })
			runtime := service.NewRuntime(core)
			previous := service.DefaultRuntime()
			service.SetDefaultRuntime(runtime)
			t.Cleanup(func() { service.SetDefaultRuntime(previous) })
			t.Cleanup(func() { web.StopTestRouterWriters(t, runtime) })
			lifecycle := supervisor.New(componenthost.Deps{})
			if err := lifecycle.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = lifecycle.Stop(context.Background()) })
			password := rand.Text()
			users := service.UserService{Runtime: runtime}
			if err := users.UpdateFirstUser("admin", password); err != nil {
				t.Fatal(err)
			}
			if err := db.Table("users").Where("id = ?", 1).Update("force_password_reset", false).Error; err != nil {
				t.Fatal(err)
			}
			router, err := web.NewTestRouter(runtime)
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewUnstartedServer(router)
			server.Config.WriteTimeout = time.Second
			if useHTTP2 {
				server.EnableHTTP2 = true
				server.StartTLS()
			} else {
				server.Start()
			}
			t.Cleanup(server.Close)
			authServer := httptest.NewServer(router)
			t.Cleanup(authServer.Close)
			web.LoginTestRouterAtBase(t, authServer.Client(), authServer.URL, password, base)
			server.Client().Jar = authServer.Client().Jar
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			endpoint := server.URL + base + "api/"
			csrf, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"csrf", nil)
			get, err := server.Client().Do(csrf)
			if err != nil {
				t.Fatal(err)
			}
			var token struct{ Obj struct{ Token string } }
			decodeErr := json.NewDecoder(io.LimitReader(get.Body, 16384)).Decode(&token)
			_ = get.Body.Close()
			if decodeErr != nil || token.Obj.Token == "" {
				t.Fatal("missing session CSRF token", decodeErr)
			}
			post := func(route string, body any) *http.Response {
				t.Helper()
				data, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"components/blockcheck/run/"+route, bytes.NewReader(data))
				req.Header.Set("Origin", server.URL)
				req.Header.Set("X-CSRF-Token", token.Obj.Token)
				req.Header.Set("Content-Type", "application/json")
				if route == "stream" {
					req.Header.Set("Accept-Encoding", "gzip")
				}
				response, err := server.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				return response
			}
			groups, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"components/blockcheck/groups", nil)
			catalogResponse, err := server.Client().Do(groups)
			if err != nil {
				t.Fatal(err)
			}
			var catalog struct{ Obj struct{ Revision string } }
			decodeErr = json.NewDecoder(io.LimitReader(catalogResponse.Body, 16384)).Decode(&catalog)
			_ = catalogResponse.Body.Close()
			if decodeErr != nil || catalogResponse.StatusCode != http.StatusOK || catalog.Obj.Revision == "" {
				t.Fatal("catalog fixture was not admitted", decodeErr)
			}
			request := map[string]any{"all": true, "catalogRevision": catalog.Obj.Revision, "family": "ipv4",
				"path": map[string]string{"mode": "OUTBOUND", "ref": "fixture"}}
			stream := post("stream", request)
			defer stream.Body.Close()
			if stream.StatusCode != http.StatusOK || stream.Header.Get("Content-Encoding") != "" ||
				stream.Header.Get("Cache-Control") != "no-store" || stream.Header.Get("X-Accel-Buffering") != "no" ||
				!strings.HasPrefix(stream.Header.Get("Content-Type"), "application/x-ndjson") || useHTTP2 && stream.ProtoMajor != 2 {
				t.Fatalf("wrong stream transport: %d %s %v", stream.StatusCode, stream.Proto, stream.Header)
			}
			decoder := json.NewDecoder(io.LimitReader(stream.Body, 64<<10))
			var started suiteFrame
			if err := decoder.Decode(&started); err != nil || started.Kind != "started" || !started.Job.CancelSupported {
				t.Fatalf("missing cancellable operation: %+v %v", started, err)
			}
			select {
			case <-connected:
			case <-ctx.Done():
				t.Fatal("the suite never reached the controlled loopback proxy")
			}
			cancelJob := func(id string, success bool) {
				t.Helper()
				res := post("cancel", map[string]string{"operationId": id})
				defer res.Body.Close()
				var body struct{ Success bool }
				if err := json.NewDecoder(io.LimitReader(res.Body, 16384)).Decode(&body); err != nil || res.StatusCode != http.StatusOK || body.Success != success {
					t.Fatalf("wrong cancellation disposition: %d %v %v", res.StatusCode, body.Success, err)
				}
			}
			cancelJob("nolonger-current", false)
			// Cross the listener's deadline while no new stream frames are being sent.
			time.Sleep(1200 * time.Millisecond)
			cancelJob(started.Job.OperationID, true)
			var last suiteFrame
			for {
				var event suiteFrame
				if err := decoder.Decode(&event); err != nil {
					if err != io.EOF {
						t.Fatal("stream lost clean EOF", err)
					}
					break
				}
				last = event
			}
			if last.Kind != "finished" || last.Job.Phase != "CANCELED" || last.Progress.GroupsDone != 1 || last.Progress.ProbesDone != 1 {
				t.Fatalf("cancellation discarded prior work or lost the terminal frame: %+v", last)
			}
		})
	}
}
