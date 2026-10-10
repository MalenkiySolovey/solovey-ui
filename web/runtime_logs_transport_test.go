package web

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

// Use the real router, including gzip, authorization and request budgets. The
// API-only tests do not exercise the production compression wrapper.
func TestRuntimeLogsProductionMiddlewarePreservesTransportDeadlines(t *testing.T) {
	for _, protocol := range []string{"http1", "http2"} {
		for _, namespace := range []string{"api", "apiv2"} {
			t.Run(protocol+"/"+namespace, func(t *testing.T) {
				db := initWebRouterTestDB(t)
				// A literal metacharacter proves exclusion follows the configured
				// base path rather than interpreting it as an operator-supplied regex.
				setWebRouterSetting(t, db, "webPath", "/qualification+v1/")
				if err := db.Table("users").Where("id = ?", 1).Update("force_password_reset", false).Error; err != nil {
					t.Fatal(err)
				}
				core := coreruntime.NewCore()
				if err := core.Start([]byte(`{"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = core.Stop() })
				runtime := service.NewRuntime(core)
				users := service.UserService{Runtime: runtime}
				password := rand.Text()
				if namespace == "api" {
					if err := users.UpdateFirstUser("admin", password); err != nil {
						t.Fatal(err)
					}
				}
				token, err := users.AddToken("admin", 0, "transport fixture", "read")
				if err != nil {
					t.Fatal(err)
				}
				panel, err := NewServer(WithRuntime(runtime))
				if err != nil {
					t.Fatal(err)
				}
				router, err := panel.initRouter()
				if err != nil {
					t.Fatal(err)
				}
				server := httptest.NewUnstartedServer(router)
				server.Config.WriteTimeout = time.Second
				if protocol == "http2" {
					server.EnableHTTP2 = true
					server.StartTLS()
				} else {
					server.Start()
				}
				t.Cleanup(server.Close)
				if namespace == "api" {
					loginRuntimeTransportFixture(t, server.Client(), server.URL, password)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
				defer cancel()
				generation := core.RuntimeStatus(ctx).Generation
				endpoint := server.URL + "/qualification+v1/" + namespace + "/runtime/logs?generation=" + url.QueryEscape(generation)
				request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
				if namespace == "apiv2" {
					request.Header.Set("Authorization", "Bearer "+token)
				}
				request.Header.Set("Accept-Encoding", "gzip")
				response, err := server.Client().Do(request)
				if err != nil {
					t.Fatal("stream transport failed", err)
				}
				defer response.Body.Close()
				if response.StatusCode != http.StatusOK || response.Header.Get("Content-Encoding") != "" || response.Header.Get("Cache-Control") != "no-store" {
					t.Fatalf("authorized runtime stream compressed or rejected: status=%d compressed=%t", response.StatusCode, response.Header.Get("Content-Encoding") != "")
				}
				if protocol == "http2" && response.ProtoMajor != 2 {
					t.Fatal("native HTTP2 stream was not negotiated")
				}
				reader := bufio.NewReader(response.Body)
				if _, err := reader.ReadBytes('\n'); err != nil {
					t.Fatal("initial owner frame unavailable", err)
				}
				// Cross the real server's original write deadline, then stop the
				// generation through its public owner to produce a terminal frame.
				// Receiving that frame and clean EOF is the assertion, not elapsed time.
				timer := time.NewTimer(1200 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-ctx.Done():
					t.Fatal("idle barrier exceeded")
				}
				if err := core.Stop(); err != nil {
					t.Fatal(err)
				}
				tail, readErr := io.ReadAll(io.LimitReader(reader, 64*1024+1))
				if readErr != nil || len(tail) > 64*1024 || !strings.Contains(string(tail), `"closed":true`) {
					t.Fatal("idle stream lost terminal frame or clean EOF", readErr)
				}
				// Ordinary responses still use the existing compression policy.
				ordinary, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/qualification+v1/login", nil)
				ordinary.Header.Set("Accept-Encoding", "gzip")
				page, err := server.Client().Do(ordinary)
				if err != nil {
					t.Fatal(err)
				}
				defer page.Body.Close()
				if page.StatusCode != http.StatusOK || page.Header.Get("Content-Encoding") != "gzip" {
					t.Fatal("ordinary compression policy changed")
				}
			})
		}
	}
}

// Legacy API uses browser sessions. Exercise the existing CSRF/password login
// owner; never enable API-token authentication merely to satisfy this fixture.
func loginRuntimeTransportFixture(t *testing.T, client *http.Client, endpoint, password string) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar = jar
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	csrfRequest, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/qualification+v1/api/csrf", nil)
	csrfResponse, err := client.Do(csrfRequest)
	if err != nil {
		t.Fatal("browser CSRF request failed")
	}
	var csrf struct {
		Obj struct {
			Token string `json:"token"`
		} `json:"obj"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(csrfResponse.Body, 16*1024)).Decode(&csrf)
	_ = csrfResponse.Body.Close()
	if decodeErr != nil || csrfResponse.StatusCode != 200 || csrf.Obj.Token == "" {
		t.Fatal("browser CSRF owner rejected fixture")
	}
	form := url.Values{"user": {"admin"}, "pass": {password}}
	login, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/qualification+v1/api/login", strings.NewReader(form.Encode()))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login.Header.Set("Origin", endpoint)
	login.Header.Set("X-CSRF-Token", csrf.Obj.Token)
	response, err := client.Do(login)
	if err != nil {
		t.Fatal("browser password request failed")
	}
	var result struct {
		Success bool `json:"success"`
	}
	decodeErr = json.NewDecoder(io.LimitReader(response.Body, 16*1024)).Decode(&result)
	_ = response.Body.Close()
	if decodeErr != nil || response.StatusCode != 200 || !result.Success {
		t.Fatal("browser login owner rejected fixture")
	}
}
