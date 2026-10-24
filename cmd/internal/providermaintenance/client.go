// Package providermaintenance is the administrative CLI transport for Update.
// It owns no database, provider selection, activation or authentication policy.
package providermaintenance

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/actions"
	"github.com/MalenkiySolovey/solovey-ui/internal/release"
	updateservice "github.com/MalenkiySolovey/solovey-ui/service/update"
)

const maxResponseBytes = 1 << 20

var errRequest = errors.New("provider maintenance request failed; inspect the authenticated Operations status")

type plan struct {
	Preview            json.RawMessage                         `json:"preview"`
	Request            updateservice.ProviderTransitionRequest `json:"request"`
	Confirmation       string                                  `json:"confirmation"`
	ActionID           string                                  `json:"actionId"`
	StepUpTargetDigest string                                  `json:"stepUpTargetDigest"`
}

type client struct {
	http  *http.Client
	base  *url.URL
	csrf  string
	input *json.Decoder
	log   io.Writer
}

func Run(args []string, input io.Reader, output, diagnostics io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(diagnostics, "usage: core-provider status|preview|migrate --api-url <Panel API URL>; credentials are read as JSON from stdin")
		return 2
	}
	flags := flag.NewFlagSet("core-provider "+args[0], flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	apiURL := flags.String("api-url", "", "Panel API base URL, including /api")
	channel := flags.String("channel", "main", "signed release channel: main or beta")
	target := flags.String("target-provider", "", "explicit target: sing-box or shtorm-extended")
	planPath := flags.String("plan", "", "reviewed JSON plan returned by preview")
	confirmation := flags.String("confirm", "", "exact confirmation returned by preview")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		return 2
	}
	visited := map[string]bool{}
	flags.Visit(func(flag *flag.Flag) { visited[flag.Name] = true })
	if (*channel != "main" && *channel != "beta") ||
		(args[0] == "status" && (visited["target-provider"] || visited["plan"] || visited["confirm"])) ||
		(args[0] == "preview" && ((*target != "sing-box" && *target != "shtorm-extended") || visited["plan"] || visited["confirm"])) ||
		(args[0] == "migrate" && (*planPath == "" || *confirmation == "" || visited["channel"] || visited["target-provider"])) ||
		(args[0] != "status" && args[0] != "preview" && args[0] != "migrate") {
		fmt.Fprintln(diagnostics, "invalid core-provider command or maintenance plan arguments")
		return 2
	}
	client, err := newClient(*apiURL, input, diagnostics)
	if err != nil {
		fmt.Fprintln(diagnostics, err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var reviewed plan
	if args[0] == "migrate" {
		file, openErr := os.Open(*planPath)
		if openErr != nil {
			fmt.Fprintln(diagnostics, "reviewed maintenance plan is unavailable")
			return 2
		}
		decodeErr := decodeOne(io.LimitReader(file, 256<<10+1), &reviewed)
		closeErr := file.Close()
		if decodeErr != nil || closeErr != nil || reviewed.validate() != nil || *confirmation != reviewed.Confirmation {
			fmt.Fprintln(diagnostics, "maintenance plan or explicit confirmation is invalid")
			return 2
		}
	}
	if err := client.login(ctx); err != nil {
		fmt.Fprintln(diagnostics, err)
		return 1
	}
	defer client.logout()
	var result json.RawMessage
	switch args[0] {
	case "status":
		result, err = client.call(ctx, http.MethodGet, "/v1/operations/update/status?channel="+*channel, nil, "", "")
	case "preview":
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err == nil {
			body, _ := json.Marshal(struct {
				Channel        release.Channel `json:"channel"`
				TargetProvider string          `json:"targetProvider"`
				IdempotencyKey string          `json:"idempotencyKey"`
			}{release.Channel(*channel), *target, "provider-maintenance:" + hex.EncodeToString(nonce[:])})
			result, err = client.call(ctx, http.MethodPost, "/v1/operations/update/provider/preview", body, "application/json", "")
		}
	case "migrate":
		var grant string
		grant, err = client.stepUp(ctx, reviewed)
		if err == nil {
			body, _ := json.Marshal(struct {
				updateservice.ProviderTransitionRequest
				Confirmation string `json:"confirmation"`
			}{reviewed.Request, reviewed.Confirmation})
			result, err = client.call(ctx, http.MethodPost, "/v1/operations/update/provider/maintain", body, "application/json", grant)
		}
	}
	if err != nil {
		fmt.Fprintln(diagnostics, errRequest)
		return 1
	}
	if args[0] == "preview" {
		var generated plan
		if decodeOne(bytes.NewReader(result), &generated) != nil || generated.validate() != nil {
			fmt.Fprintln(diagnostics, errRequest)
			return 1
		}
	}
	if !json.Valid(result) {
		fmt.Fprintln(diagnostics, errRequest)
		return 1
	}
	_, err = fmt.Fprintln(output, string(result))
	if err != nil {
		return 1
	}
	return 0
}

func newClient(address string, input io.Reader, diagnostics io.Writer) (*client, error) {
	base, err := url.Parse(address)
	if err != nil || base == nil || base.Hostname() == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || base.RawPath != "" ||
		(base.Scheme != "https" && (base.Scheme != "http" || net.ParseIP(base.Hostname()) == nil || !net.ParseIP(base.Hostname()).IsLoopback())) {
		return nil, errors.New("an HTTPS Panel API URL or literal loopback HTTP URL is required")
	}
	base.Path = strings.TrimRight(base.Path, "/")
	if !strings.HasSuffix(base.Path, "/api") {
		return nil, errors.New("panel API URL must include its configured base path and /api")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, errRequest
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &client{base: base, input: json.NewDecoder(io.LimitReader(input, 64<<10)), log: diagnostics,
		http: &http.Client{Jar: jar, Transport: transport, Timeout: 3 * time.Minute,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (client *client) login(ctx context.Context) error {
	fmt.Fprintln(client.log, "Login JSON from stdin: username, password.")
	var credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	client.input.DisallowUnknownFields()
	if client.input.Decode(&credentials) != nil || credentials.Username == "" || credentials.Password == "" {
		return errors.New("login credentials are required on stdin")
	}
	// Login is protected by the same session-bound CSRF policy as every write.
	// Establish that pre-authentication session before submitting credentials.
	if client.refreshCSRF(ctx) != nil {
		return errRequest
	}
	form := url.Values{"user": {credentials.Username}, "pass": {credentials.Password}, "remember": {"false"}}
	raw, err := client.call(ctx, http.MethodPost, "/login", []byte(form.Encode()), "application/x-www-form-urlencoded", "")
	credentials.Username, credentials.Password = "", ""
	if err != nil {
		return errRequest
	}
	var state struct {
		State string `json:"state"`
	}
	if json.Unmarshal(raw, &state) != nil || client.refreshCSRF(ctx) != nil {
		return errRequest
	}
	if state.State == "mfa_pending" {
		proof, err := client.readProof("Login MFA JSON from stdin: method (totp or recovery), credential.")
		if err != nil || (proof.Method != "totp" && proof.Method != "recovery") {
			return errRequest
		}
		endpoint := "/v1/security/mfa/challenge"
		if proof.Method == "recovery" {
			endpoint = "/v1/security/mfa/recovery"
		}
		body, _ := json.Marshal(struct {
			Code string `json:"code"`
		}{proof.Credential})
		if _, err := client.call(ctx, http.MethodPost, endpoint, body, "application/json", ""); err != nil || client.refreshCSRF(ctx) != nil {
			return errRequest
		}
	} else if state.State != "authenticated" {
		return errors.New("complete the existing Panel security recovery workflow before maintenance")
	}
	return nil
}

type credentialProof struct {
	Method     string `json:"method"`
	Credential string `json:"credential"`
}

func (client *client) readProof(prompt string) (credentialProof, error) {
	fmt.Fprintln(client.log, prompt)
	var proof credentialProof
	if client.input.Decode(&proof) != nil || proof.Credential == "" || (proof.Method != "password" && proof.Method != "totp" && proof.Method != "recovery") {
		return proof, errRequest
	}
	return proof, nil
}

func (client *client) stepUp(ctx context.Context, reviewed plan) (string, error) {
	proof, err := client.readProof("Step-up JSON from stdin: method, credential. Supply a fresh MFA code when MFA is enabled.")
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(struct {
		Method        string `json:"method"`
		Credential    string `json:"credential"`
		OperationKind string `json:"operationKind"`
		TargetDigest  string `json:"targetDigest"`
	}{proof.Method, proof.Credential, actions.ProviderMaintenance, reviewed.StepUpTargetDigest})
	raw, err := client.call(ctx, http.MethodPost, "/v1/security/step-up", body, "application/json", "")
	var grant struct {
		Token string `json:"token"`
	}
	if err != nil || json.Unmarshal(raw, &grant) != nil || grant.Token == "" || client.refreshCSRF(ctx) != nil {
		return "", errRequest
	}
	return grant.Token, nil
}

func (client *client) refreshCSRF(ctx context.Context) error {
	raw, err := client.call(ctx, http.MethodGet, "/csrf", nil, "", "")
	var response struct {
		Token string `json:"token"`
	}
	if err != nil || json.Unmarshal(raw, &response) != nil || response.Token == "" {
		return errRequest
	}
	client.csrf = response.Token
	return nil
}

func (client *client) logout() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = client.call(ctx, http.MethodPost, "/logout", nil, "application/json", "")
	client.http.CloseIdleConnections()
}

func (client *client) call(ctx context.Context, method, endpoint string, body []byte, contentType, grant string) (json.RawMessage, error) {
	address := *client.base
	path, query, _ := strings.Cut(endpoint, "?")
	address.Path, address.RawQuery = client.base.Path+path, query
	request, err := http.NewRequestWithContext(ctx, method, address.String(), bytes.NewReader(body))
	if err != nil {
		return nil, errRequest
	}
	request.Header.Set("User-Agent", "Solovey-UI-Provider-Maintenance-CLI/1")
	request.Header.Set("Origin", client.base.Scheme+"://"+client.base.Host)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if client.csrf != "" {
		request.Header.Set("X-CSRF-Token", client.csrf)
	}
	if grant != "" {
		request.Header.Set("X-Step-Up-Token", grant)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return nil, errRequest
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	var envelope struct {
		Success bool            `json:"success"`
		Obj     json.RawMessage `json:"obj"`
	}
	if err != nil || len(raw) > maxResponseBytes || response.StatusCode != http.StatusOK || json.Unmarshal(raw, &envelope) != nil || !envelope.Success {
		return nil, errRequest
	}
	return envelope.Obj, nil
}

func (reviewed plan) validate() error {
	invocation := updateservice.ProviderTransitionAction(reviewed.Request)
	digest := sha256.Sum256([]byte(invocation.Target))
	if reviewed.Request.Validate() != nil || invocation.Validate() != nil || reviewed.ActionID != actions.ProviderMaintenance ||
		reviewed.Confirmation != updateservice.ProviderTransitionConfirmation(reviewed.Request) || reviewed.StepUpTargetDigest != hex.EncodeToString(digest[:]) {
		return errRequest
	}
	return nil
}

func decodeOne(input io.Reader, value any) error {
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errRequest
	}
	return nil
}
