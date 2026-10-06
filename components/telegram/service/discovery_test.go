//go:build !minimal

package telegram_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	telegramservice "github.com/MalenkiySolovey/solovey-ui/components/telegram/service"
)

const discoveryTestToken = "123456:discovery-test-token"

type discoverySettings struct {
	testTelegramSettings
	token      string
	tokenError error
	reads      atomic.Int32
}

func (s *discoverySettings) GetTelegramBotToken() (string, error) {
	s.reads.Add(1)
	return s.token, s.tokenError
}

type discoveryRoundTrip func(*http.Request) (*http.Response, error)

func (f discoveryRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A controllable parent deadline proves in-flight classification without a
// sleep or changing the production ten-second timer.
type controlledDiscoveryDeadline struct {
	context.Context
	done    chan struct{}
	expired atomic.Bool
}

func (c *controlledDiscoveryDeadline) Done() <-chan struct{} { return c.done }
func (c *controlledDiscoveryDeadline) Err() error {
	if c.expired.Load() {
		return context.DeadlineExceeded
	}
	return nil
}
func (c *controlledDiscoveryDeadline) expire() {
	if c.expired.CompareAndSwap(false, true) {
		close(c.done)
	}
}
func discoveryResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
func assertDiscoveryRedacted(t *testing.T, result telegramservice.DiscoveryResult, secrets ...string) {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(string(data), secret) {
			t.Fatal("discovery result disclosed request/provider material")
		}
	}
}

func TestDiscoverySuppliedTokenOneBoundedRequestDoesNotPersist(t *testing.T) {
	initSettingTestDB(t)
	settings := &discoverySettings{token: "654321:stored-test-token"}
	var calls atomic.Int32
	client := &http.Client{Transport: discoveryRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Scheme != "https" || r.URL.Host != "api.telegram.org" || r.URL.Path != "/bot"+discoveryTestToken+"/getUpdates" || r.Method != http.MethodGet {
			t.Error("unexpected fixed provider operation")
		}
		if r.URL.Query().Get("limit") != "100" || r.URL.Query().Get("timeout") != "0" || r.URL.Query().Has("offset") || len(r.URL.Query()) != 2 {
			t.Error("discovery must perform one bounded unacknowledged batch")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			t.Error("missing bounded request deadline")
		}
		return discoveryResponse(http.StatusOK, `{"ok":true,"result":[{"update_id":1,"message":{"chat":{"id":-1001234567890123456,"type":"supergroup","title":"private-test-title"},"text":"private-test-body"}}]}`), nil
	})}
	originalRedirect := func(*http.Request, []*http.Request) error { return errors.New("original redirect policy") }
	client.CheckRedirect = originalRedirect
	service := &telegramservice.Service{Settings: settings, Client: client}
	result := service.DetectChatContext(context.Background(), " "+discoveryTestToken+" ")
	if !result.Success || result.ChatID != "-1001234567890123456" || calls.Load() != 1 || settings.reads.Load() != 0 || settings.token != "654321:stored-test-token" {
		t.Fatalf("unexpected discovery contract: %#v calls=%d stored reads=%d", result, calls.Load(), settings.reads.Load())
	}
	if client.Timeout != 0 || client.CheckRedirect(nil, nil).Error() != "original redirect policy" {
		t.Fatal("discovery mutated the injected client")
	}
	assertDiscoveryRedacted(t, result, discoveryTestToken, "private-test-title", "private-test-body")
}

func TestDiscoveryStoredFallbackAndMissingToken(t *testing.T) {
	initSettingTestDB(t)
	settings := &discoverySettings{token: discoveryTestToken}
	var calls atomic.Int32
	service := &telegramservice.Service{Settings: settings, Client: &http.Client{Transport: discoveryRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if !strings.Contains(r.URL.Path, discoveryTestToken) {
			t.Error("stored token not used")
		}
		return discoveryResponse(200, `{"ok":true,"result":[{"update_id":0,"channel_post":{"chat":{"id":-99,"type":"channel"}}}]}`), nil
	})}}
	result := service.DetectChatContext(context.Background(), "")
	if !result.Success || result.ChatID != "-99" || settings.reads.Load() != 1 {
		t.Fatalf("stored fallback: %#v", result)
	}
	settings.token = ""
	result = service.DetectChatContext(context.Background(), "  ")
	if result.ErrorClass != "missing_token" || calls.Load() != 1 {
		t.Fatalf("missing token must not call provider: %#v", result)
	}
	settings.tokenError = errors.New(discoveryTestToken)
	result = service.DetectChatContext(context.Background(), "")
	if result.ErrorClass != "settings" {
		t.Fatalf("settings error: %#v", result)
	}
	assertDiscoveryRedacted(t, result, discoveryTestToken)
	if (&telegramservice.Service{}).DetectChatContext(context.Background(), discoveryTestToken).ErrorClass != "settings" {
		t.Fatal("missing settings must fail closed")
	}
	// Invalid inputs are fixtures: the boundary must reject a missing context.
	for _, requestContext := range []context.Context{nil} {
		if service.DetectChatContext(requestContext, discoveryTestToken).ErrorClass != "request" {
			t.Fatal("nil context must fail closed")
		}
	}
}

func TestDiscoveryFakeHTTPResponsesAndLastUsableSelection(t *testing.T) {
	initSettingTestDB(t)
	update := func(id int, kind, chatType string, chatID int64) string {
		return fmt.Sprintf(`{"update_id":%d,"%s":{"chat":{"id":%d,"type":"%s"}}}`, id, kind, chatID, chatType)
	}
	updates := []string{update(1, "message", "private", 1), update(5, "edited_message", "group", -5), update(3, "channel_post", "channel", -3), update(8, "edited_channel_post", "channel", -8), update(10, "my_chat_member", "supergroup", -10), update(12, "message", "unsupported", 12), update(11, "message", "private", 0), update(10, "message", "private", 100), update(2, "message", "private", 200)}
	bounded := make([]string, 100)
	for i := range bounded {
		bounded[i] = update(i, "message", "private", int64(i+1))
	}
	cases := []struct {
		name, body  string
		status      int
		chat, class string
	}{
		{"deterministic highest usable and last tie", `{"ok":true,"result":[` + strings.Join(updates, ",") + `]}`, 200, "100", ""},
		{"exact bounded count", `{"ok":true,"result":[` + strings.Join(bounded, ",") + `]}`, 200, "100", ""},
		{"excess updates rejected", `{"ok":true,"result":[` + strings.Join(append(bounded, update(101, "message", "private", 101)), ",") + `]}`, 200, "", "response"},
		{"malformed", "not JSON " + discoveryTestToken, 200, "", "response"},
		{"missing ok", `{"result":[]}`, 200, "", "response"},
		{"non boolean ok", `{"ok":"true","result":[]}`, 200, "", "response"},
		{"null result", `{"ok":true,"result":null}`, 200, "", "response"},
		{"object result", `{"ok":true,"result":{}}`, 200, "", "response"},
		{"bad id", `{"ok":true,"result":[{"update_id":1,"message":{"chat":{"id":"123","type":"private"}}}]}`, 200, "", "response"},
		{"empty", `{"ok":true,"result":[]}`, 200, "", "no_chat"},
		{"unsupported", `{"ok":true,"result":[{"update_id":1,"callback_query":{"message":{"chat":{"id":1,"type":"private"}}}},{"message":{"chat":{"id":2,"type":"private"}}},{"update_id":-1,"message":{"chat":{"id":3,"type":"private"}}}]}`, 200, "", "no_chat"},
		{"telegram error", `{"ok":false,"error_code":401,"description":"` + discoveryTestToken + `"}`, 200, "", "unauthorized"},
		{"HTTP unauthorized", discoveryTestToken, 401, "", "unauthorized"},
		{"HTTP rate limit", discoveryTestToken, 429, "", "rate_limited"},
		{"HTTP error", discoveryTestToken, 503, "", "telegram_error"},
		{"oversized", strings.Repeat("x", (1<<20)+1), 200, "", "request"},
	}
	for _, kind := range []string{"message", "edited_message", "channel_post", "edited_channel_post", "my_chat_member"} {
		cases = append(cases, struct {
			name, body  string
			status      int
			chat, class string
		}{"supported " + kind, `{"ok":true,"result":[` + update(7, kind, "group", -77) + `]}`, 200, "-77", ""})
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			result := (&telegramservice.Service{Settings: &discoverySettings{}, Client: &http.Client{Transport: telegramServerRoundTripper{base: base, transport: server.Client().Transport}}}).DetectChatContext(context.Background(), discoveryTestToken)
			if result.Success != (tt.chat != "") || result.ChatID != tt.chat || result.ErrorClass != tt.class || calls.Load() != 1 {
				t.Fatalf("unexpected bounded result: %#v calls=%d", result, calls.Load())
			}
			assertDiscoveryRedacted(t, result, discoveryTestToken)
		})
	}
}

func TestDiscoveryCancellationDeadlineNetworkAndRedirect(t *testing.T) {
	initSettingTestDB(t)
	entered := make(chan struct{})
	service := &telegramservice.Service{Settings: &discoverySettings{}, Client: &http.Client{Transport: discoveryRoundTrip(func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-r.Context().Done()
		return nil, errors.New(discoveryTestToken)
	})}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultCh := make(chan telegramservice.DiscoveryResult, 1)
	go func() { resultCh <- service.DetectChatContext(ctx, discoveryTestToken) }()
	waitForTestChannel(t, entered, 5*time.Second, "discovery did not enter transport")
	cancel()
	select {
	case result := <-resultCh:
		if result.ErrorClass != "canceled" {
			t.Fatalf("cancel: %#v", result)
		}
		assertDiscoveryRedacted(t, result, discoveryTestToken)
	case <-time.After(5 * time.Second):
		t.Fatal("canceled discovery did not drain")
	}
	expired, done := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer done()
	if result := service.DetectChatContext(expired, discoveryTestToken); result.ErrorClass != "timeout" {
		t.Fatalf("deadline: %#v", result)
	}
	service.Client = &http.Client{Transport: discoveryRoundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New(discoveryTestToken) })}
	if result := service.DetectChatContext(context.Background(), discoveryTestToken); result.ErrorClass != "network" {
		t.Fatalf("network: %#v", result)
	}
	var redirects atomic.Int32
	service.Client = &http.Client{Transport: discoveryRoundTrip(func(*http.Request) (*http.Response, error) {
		redirects.Add(1)
		r := discoveryResponse(302, discoveryTestToken)
		r.Header.Set("Location", "https://example.invalid/steal")
		return r, nil
	})}
	result := service.DetectChatContext(context.Background(), discoveryTestToken)
	if result.ErrorClass != "telegram_error" || redirects.Load() != 1 {
		t.Fatalf("redirect should not forward token: %#v calls=%d", result, redirects.Load())
	}
	assertDiscoveryRedacted(t, result, discoveryTestToken)
}

func TestDiscoveryInFlightDeadlineIsTimeoutAndDrains(t *testing.T) {
	initSettingTestDB(t)
	entered := make(chan struct{})
	parent := &controlledDiscoveryDeadline{Context: context.Background(), done: make(chan struct{})}
	defer parent.expire()
	service := &telegramservice.Service{Settings: &discoverySettings{}, Client: &http.Client{Transport: discoveryRoundTrip(func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-r.Context().Done()
		return nil, errors.New(discoveryTestToken)
	})}}
	resultCh := make(chan telegramservice.DiscoveryResult, 1)
	go func() { resultCh <- service.DetectChatContext(parent, discoveryTestToken) }()
	waitForTestChannel(t, entered, 5*time.Second, "deadline fixture did not enter HTTP transport")
	parent.expire()
	select {
	case result := <-resultCh:
		if result.ErrorClass != "timeout" {
			t.Fatalf("in-flight deadline: %#v", result)
		}
		assertDiscoveryRedacted(t, result, discoveryTestToken)
	case <-time.After(5 * time.Second):
		t.Fatal("deadline request did not drain")
	}
}

func TestDiscoveryConcurrentOperationsUseRequestLocalTokenAndClient(t *testing.T) {
	initSettingTestDB(t)
	var calls atomic.Int32
	service := &telegramservice.Service{Settings: &discoverySettings{}, Client: &http.Client{Transport: discoveryRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return discoveryResponse(200, `{"ok":true,"result":[{"update_id":1,"message":{"chat":{"id":9,"type":"private"}}}]}`), nil
	})}}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Go(func() {
			result := service.DetectChatContext(context.Background(), discoveryTestToken)
			if !result.Success || result.ChatID != "9" {
				t.Errorf("concurrent result: %#v", result)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 16 {
		t.Fatalf("unexpected operation count %d", calls.Load())
	}
}
