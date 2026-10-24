package providermaintenance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	updateservice "github.com/MalenkiySolovey/solovey-ui/service/update"
)

func planFixture() plan {
	request := updateservice.ProviderTransitionRequest{Channel: "main", TargetProvider: "shtorm-extended", ExpectedSequence: 19,
		ExpectedManifestDigest: strings.Repeat("a", 64), ExpectedArtifactSetDigest: strings.Repeat("b", 64),
		ExpectedConfigurationRevision: strings.Repeat("c", 64), ExpectedProviderRevision: strings.Repeat("d", 64),
		IdempotencyKey: "provider-maintenance:test", Acknowledged: true}
	return planFor(request)
}

func planFor(request updateservice.ProviderTransitionRequest) plan {
	invocation := updateservice.ProviderTransitionAction(request)
	digest := sha256.Sum256([]byte(invocation.Target))
	return plan{Preview: json.RawMessage(`{"state":"TARGET_VALIDATION_REQUIRED"}`), Request: request,
		Confirmation: updateservice.ProviderTransitionConfirmation(request), ActionID: invocation.ID, StepUpTargetDigest: hex.EncodeToString(digest[:])}
}

func TestCLIUsesSessionMFAFreshCSRFAndExactOneUseAction(t *testing.T) {
	reviewed := planFixture()
	path := filepath.Join(t.TempDir(), "plan.json")
	raw, _ := json.Marshal(reviewed)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var calls []string
	var mu sync.Mutex
	csrf := ""
	csrfCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		endpoint := strings.TrimPrefix(request.URL.Path, "/base/api")
		calls = append(calls, endpoint)
		if request.Header.Get("Authorization") != "" || request.Header.Get("User-Agent") != "Solovey-UI-Provider-Maintenance-CLI/1" {
			t.Error("CLI introduced token authentication or changed its session identity")
		}
		bootstrap := endpoint == "/csrf" && csrfCount == 0
		if !bootstrap {
			if _, err := request.Cookie("s-ui"); err != nil {
				t.Error("session cookie missing")
			}
			if request.Method == http.MethodPost && request.Header.Get("X-CSRF-Token") != csrf {
				t.Error("stale CSRF token")
			}
		}
		obj := any(map[string]any{})
		switch endpoint {
		case "/login":
			if request.FormValue("user") != "operator" || request.FormValue("pass") != "password-sentinel" {
				t.Error("login transport changed")
			}
			http.SetCookie(w, &http.Cookie{Name: "s-ui", Value: "cookie-sentinel", Path: "/", HttpOnly: true})
			obj = map[string]any{"state": "mfa_pending"}
		case "/csrf":
			if bootstrap {
				http.SetCookie(w, &http.Cookie{Name: "s-ui", Value: "preauth-cookie-sentinel", Path: "/", HttpOnly: true})
			}
			csrfCount++
			csrf = fmt.Sprintf("csrf-%d", csrfCount)
			obj = map[string]any{"token": csrf}
		case "/v1/security/mfa/challenge":
			var proof struct {
				Code string `json:"code"`
			}
			if json.NewDecoder(request.Body).Decode(&proof) != nil || proof.Code != "mfa-sentinel" {
				t.Error("login MFA was bypassed")
			}
		case "/v1/security/step-up":
			var proof struct{ Method, Credential, OperationKind, TargetDigest string }
			if json.NewDecoder(request.Body).Decode(&proof) != nil || proof.Method != "totp" || proof.Credential != "fresh-stepup-sentinel" ||
				proof.OperationKind != actions.ProviderMaintenance || proof.TargetDigest != reviewed.StepUpTargetDigest {
				t.Error("step-up did not bind the reviewed owner action")
			}
			obj = map[string]any{"token": "grant-sentinel"}
		case "/v1/operations/update/provider/maintain":
			var actual struct {
				updateservice.ProviderTransitionRequest
				Confirmation string `json:"confirmation"`
			}
			if json.NewDecoder(request.Body).Decode(&actual) != nil || request.Header.Get("X-Step-Up-Token") != "grant-sentinel" ||
				updateservice.ProviderTransitionAction(actual.ProviderTransitionRequest) != updateservice.ProviderTransitionAction(reviewed.Request) || actual.Confirmation != reviewed.Confirmation {
				t.Error("CLI changed the approved request or omitted one-use assurance")
			}
			obj = map[string]any{"state": "VERIFYING_ACTIVE", "operationId": "update-operation:fixture"}
		case "/logout":
		default:
			t.Error("unexpected CLI endpoint", endpoint)
			w.WriteHeader(http.StatusNotFound)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "obj": obj})
	}))
	defer server.Close()
	input := strings.NewReader("{\"username\":\"operator\",\"password\":\"password-sentinel\"}\n" +
		"{\"method\":\"totp\",\"credential\":\"mfa-sentinel\"}\n" +
		"{\"method\":\"totp\",\"credential\":\"fresh-stepup-sentinel\"}\n")
	var output, diagnostics bytes.Buffer
	if code := Run([]string{"migrate", "--api-url", server.URL + "/base/api", "--plan", path, "--confirm", reviewed.Confirmation}, input, &output, &diagnostics); code != 0 {
		t.Fatal("CLI maintenance failed", code, diagnostics.String())
	}
	mu.Lock()
	actualCalls := strings.Join(calls, ",")
	mu.Unlock()
	if actualCalls != "/csrf,/login,/csrf,/v1/security/mfa/challenge,/csrf,/v1/security/step-up,/csrf,/v1/operations/update/provider/maintain,/logout" {
		t.Fatal("CLI session/assurance lifecycle changed", actualCalls)
	}
	for _, secret := range []string{"password-sentinel", "mfa-sentinel", "fresh-stepup-sentinel", "preauth-cookie-sentinel", "cookie-sentinel", "grant-sentinel"} {
		if strings.Contains(output.String()+diagnostics.String(), secret) {
			t.Fatal("credential/session material escaped CLI output")
		}
	}
}

func TestCLIBootstrapCSRFFailureCannotSubmitCredentials(t *testing.T) {
	var submitted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/csrf" || request.Method != http.MethodGet || request.ContentLength != 0 {
			submitted.Store(true)
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	var output, diagnostics bytes.Buffer
	if code := Run([]string{"status", "--api-url", server.URL + "/api"}, strings.NewReader(`{"username":"operator","password":"private-sentinel"}`), &output, &diagnostics); code != 1 || submitted.Load() || strings.Contains(output.String()+diagnostics.String(), "private-sentinel") {
		t.Fatal("failed pre-authentication CSRF submitted credentials or escaped them to output", code)
	}
}

func TestCLIRejectsChangedPlanBeforeAuthentication(t *testing.T) {
	reviewed := planFixture()
	reviewed.Request.TargetProvider = "sing-box"
	path := filepath.Join(t.TempDir(), "changed-plan.json")
	raw, _ := json.Marshal(reviewed)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called.Store(true) }))
	defer server.Close()
	var output, diagnostics bytes.Buffer
	if code := Run([]string{"migrate", "--api-url", server.URL + "/api", "--plan", path, "--confirm", reviewed.Confirmation}, strings.NewReader("private"), &output, &diagnostics); code != 2 || called.Load() {
		t.Fatal("changed plan reached authentication or mutation", code)
	}
}

func TestCLIRedirectAndRemotePlaintextFailClosed(t *testing.T) {
	for _, address := range []string{"http://remote.example/api", "http://localhost/api", "https://user:password@host/api", "https://host/api?token=private"} {
		if _, err := newClient(address, strings.NewReader(""), &bytes.Buffer{}); err == nil {
			t.Fatal("unsafe API origin was accepted")
		}
	}
	var forwarded atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	var output, diagnostics bytes.Buffer
	if code := Run([]string{"status", "--api-url", server.URL + "/api"}, strings.NewReader(`{"username":"operator","password":"private-sentinel"}`), &output, &diagnostics); code != 1 || forwarded.Load() || strings.Contains(diagnostics.String(), "private-sentinel") {
		t.Fatal("redirect forwarded credentials or leaked authentication input", code)
	}
}
