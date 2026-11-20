//go:build !minimal

package telegram_test

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-backend/storage/sqlite"
	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/componentkit/telegram"
	telegramservice "github.com/MalenkiySolovey/solovey-ui/components/telegram/service"
	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
)

func TestTelegramDisabledMakesNoOutboundCall(t *testing.T) {
	initSettingTestDB(t)
	rt := &countingRoundTripper{}

	result := (&telegramservice.Service{Settings: testTelegramSettings{}, Client: &http.Client{Transport: rt, Timeout: time.Second}}).TestTelegram()
	if result.Success || result.ErrorClass != "disabled" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if rt.Count() != 0 {
		t.Fatalf("disabled telegram made %d outbound calls", rt.Count())
	}
}

func TestNewTelegramHTTPClientUsesHTTPProxySettings(t *testing.T) {
	client, err := integrationtelegram.NewHTTPClient(integrationtelegram.ProxyConfig{
		URL:      "http://1.1.1.1:8080",
		Username: "proxy-user",
		Password: "proxy-pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatalf("expected proxy transport, got %#v", client.Transport)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.telegram.org", nil)
	if err != nil {
		t.Fatal(err)
	}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatal(err)
	}
	if proxyURL.Host != "1.1.1.1:8080" {
		t.Fatalf("unexpected proxy host: %s", proxyURL.Host)
	}
	if user := proxyURL.User.Username(); user != "proxy-user" {
		t.Fatalf("unexpected proxy user: %s", user)
	}
	if password, _ := proxyURL.User.Password(); password != "proxy-pass" {
		t.Fatalf("unexpected proxy password: %s", password)
	}
}

func TestEncryptTelegramBackupRoundTrip(t *testing.T) {
	plain := []byte("sqlite bytes")
	encrypted, key, err := backupenvelope.EncryptLegacy(plain)
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 32 {
		t.Fatalf("expected AES-256 key, got %d bytes", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	if len(encrypted) <= gcm.NonceSize() {
		t.Fatalf("encrypted backup too short: %d", len(encrypted))
	}
	nonce := encrypted[:gcm.NonceSize()]
	ciphertext := encrypted[gcm.NonceSize():]
	decrypted, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypted, plain) {
		t.Fatalf("decrypted backup mismatch: %q", decrypted)
	}
}

func TestSendTelegramDocumentSanitizesCaptionAndUsesMultipart(t *testing.T) {
	settingService := initSettingTestDB(t)
	enableTelegramForTest(t, settingService)
	rt := &captureRoundTripper{}

	result := (&telegramservice.Service{Settings: testTelegramSettings{}, Client: &http.Client{Transport: rt, Timeout: time.Second}}).SendDocument("backup.db.aes", []byte("encrypted"), "ok\r\nAuthorization: Bearer secret")
	if !result.Success {
		t.Fatalf("unexpected result: %#v", result)
	}
	if rt.req == nil || !strings.Contains(rt.req.URL.Path, "/sendDocument") {
		t.Fatalf("unexpected request: %#v", rt.req)
	}
	_, params, err := mime.ParseMediaType(rt.req.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	reader := multipart.NewReader(bytes.NewReader(rt.body), params["boundary"])
	fields := map[string]string{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		if part.FormName() == "document" {
			if part.FileName() != "backup.db.aes" || string(content) != "encrypted" {
				t.Fatalf("unexpected document part: filename=%q content=%q", part.FileName(), string(content))
			}
			continue
		}
		fields[part.FormName()] = string(content)
	}
	if fields["chat_id"] != "42" {
		t.Fatalf("missing chat id: %#v", fields)
	}
	if strings.ContainsAny(fields["caption"], "\r\n") || strings.Contains(fields["caption"], "secret") {
		t.Fatalf("caption was not sanitized/redacted: %q", fields["caption"])
	}
}

func TestNewTelegramHTTPClientAcceptsSOCKS5Proxy(t *testing.T) {
	client, err := integrationtelegram.NewHTTPClient(integrationtelegram.ProxyConfig{
		URL: "socks5://1.1.1.1:1080",
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Transport == nil {
		t.Fatal("expected socks5 transport")
	}
}

func TestTelegramSOCKS5DialReturnsOnContextCancel(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	stop := make(chan struct{})
	acceptedDone := make(chan struct{})
	go func() {
		defer close(acceptedDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		<-stop
	}()

	transport, err := integrationtelegram.NewSOCKS5Transport(listener.Addr().String(), nil)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	conn, err := transport.DialContext(ctx, "tcp", "example.com:443")
	if conn != nil {
		_ = conn.Close()
	}
	close(stop)
	_ = listener.Close()
	waitForTestChannel(t, acceptedDone, time.Second, "slow SOCKS5 server goroutine did not exit")
	if err == nil {
		t.Fatal("slow SOCKS5 dial should fail on context timeout")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("SOCKS5 dial ignored context timeout; elapsed=%s err=%v", elapsed, err)
	}
}

func TestTelegramInvalidProxySettingFailsBeforeOutbound(t *testing.T) {
	settingService := initSettingTestDB(t)
	enableTelegramForTest(t, settingService)
	if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", "telegramProxyURL").Update("value", "http://127.0.0.1:8080").Error; err != nil {
		t.Fatal(err)
	}

	result := (&telegramservice.Service{Settings: testTelegramSettings{}}).TestTelegram()
	if result.Success || result.ErrorClass != "proxy" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestTelegramStatusErrorClassMapping(t *testing.T) {
	settingService := initSettingTestDB(t)
	enableTelegramForTest(t, settingService)
	tests := []struct {
		status int
		want   string
	}{
		{status: http.StatusUnauthorized, want: "unauthorized"},
		{status: http.StatusNotFound, want: "chat_not_found"},
		{status: http.StatusTooManyRequests, want: "rate_limited"},
		{status: http.StatusInternalServerError, want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			result := (&telegramservice.Service{Settings: testTelegramSettings{}, Client: &http.Client{Transport: statusRoundTripper{status: tt.status}, Timeout: time.Second}}).TestTelegram()
			if result.Success || result.ErrorClass != tt.want {
				t.Fatalf("unexpected result for %d: %#v", tt.status, result)
			}
		})
	}
}

func TestTelegramStatusErrorClassAllowlist(t *testing.T) {
	for _, class := range []string{"unauthorized", "chat_not_found", "rate_limited", "unknown"} {
		if class == "" || strings.Contains(class, "telegram_status_") {
			t.Fatalf("invalid mapped class: %q", class)
		}
	}
}

func TestTelegramSendParsesRetryAfterFrom429Response(t *testing.T) {
	settingService := initSettingTestDB(t)
	enableTelegramForTest(t, settingService)

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; !strings.Contains(got, "/sendMessage") {
			t.Fatalf("unexpected telegram path: %s", got)
		}
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"parameters":{"retry_after":1}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	telegramService := &telegramservice.Service{Settings: testTelegramSettings{}, Client: &http.Client{
		Transport: telegramServerRoundTripper{base: baseURL, transport: http.DefaultTransport},
		Timeout:   time.Second,
	}}

	result := telegramService.Send("message")

	if requests.Load() != 1 {
		t.Fatalf("expected one telegram request, got %d", requests.Load())
	}
	if result.Success || result.ErrorClass != "rate_limited" || result.RetryAfter != time.Second {
		t.Fatalf("unexpected retry-after result: %#v", result)
	}
}

func TestTelegramRetryAfterCapsAtMax(t *testing.T) {
	got := integrationtelegram.RetryAfter(http.StatusTooManyRequests, []byte(`{"ok":false,"error_code":429,"parameters":{"retry_after":999}}`))
	if got != integrationtelegram.MaxRetryAfter {
		t.Fatalf("retry_after cap=%s, want %s", got, integrationtelegram.MaxRetryAfter)
	}
}

func TestNotifyTelegramEventReturnsBeforeSendCompletes(t *testing.T) {
	settingService := initSettingTestDB(t)
	enableTelegramForTest(t, settingService)

	sendStarted := make(chan struct{})
	releaseSend := make(chan struct{})
	sendDone := make(chan struct{})
	var startedOnce sync.Once
	var doneOnce sync.Once
	notifier := telegramservice.NewNotifier(telegramservice.QueueCapacity, func(string) telegramservice.Result {
		startedOnce.Do(func() { close(sendStarted) })
		<-releaseSend
		doneOnce.Do(func() { close(sendDone) })
		return telegramservice.Result{Success: true}
	}, func(string, map[string]any) {})
	notifier.Backoff = nil
	stopNotifierAfterTest(t, notifier)

	start := time.Now()
	(&telegramservice.Service{Settings: testTelegramSettings{}, Notifier: notifier}).NotifyEvent("login_failed", map[string]string{
		"ip": "203.0.113.10",
	})
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Fatalf("NotifyTelegramEvent blocked for %s", elapsed)
	}

	select {
	case <-sendStarted:
	case <-time.After(time.Second):
		t.Fatal("queued notification was not delivered to worker")
	}
	select {
	case <-sendDone:
		t.Fatal("send completed before release; test did not exercise async path")
	default:
	}
	close(releaseSend)
	select {
	case <-sendDone:
	case <-time.After(time.Second):
		t.Fatal("send did not complete after release")
	}
}

func TestNotifyTelegramEventRedactsSensitiveFields(t *testing.T) {
	settingService := initSettingTestDB(t)
	enableTelegramForTest(t, settingService)

	sent := make(chan string, 1)
	notifier := telegramservice.NewNotifier(telegramservice.QueueCapacity, func(text string) telegramservice.Result {
		sent <- text
		return telegramservice.Result{Success: true}
	}, func(string, map[string]any) {})
	notifier.Backoff = nil
	stopNotifierAfterTest(t, notifier)

	(&telegramservice.Service{Settings: testTelegramSettings{}, Notifier: notifier}).NotifyEvent("manual_backup", map[string]string{
		"caption":       "Authorization: Bearer secret.jwt.value",
		"telegramToken": "1234567890:" + strings.Repeat("A", 35),
	})

	got := receiveString(t, sent, "redacted notification")
	if strings.Contains(got, "secret.jwt.value") || strings.Contains(got, "1234567890:") {
		t.Fatalf("telegram notification leaked sensitive value: %q", got)
	}
	if !strings.Contains(got, "Authorization: Bearer [REDACTED]") ||
		!strings.Contains(got, "telegramToken: [REDACTED]") {
		t.Fatalf("telegram notification was not redacted: %q", got)
	}
}

func TestTelegramServiceWithoutSettingsFailsClosed(t *testing.T) {
	result := (&telegramservice.Service{}).TestTelegram()
	if result.Success || result.ErrorClass != "settings" {
		t.Fatalf("nil settings result = %#v, want settings failure", result)
	}
}

func enableTelegramForTest(t *testing.T, settingService *coreservice.SettingService) {
	t.Helper()
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{
		"telegramEnabled":  "true",
		"telegramBotToken": "123456:test-token",
		"telegramChatID":   "42",
	}
	for key, value := range settings {
		if err := dbsqlite.DB().Model(model.Setting{}).Where("key = ?", key).Update("value", value).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func stopNotifierAfterTest(t *testing.T, notifier *telegramservice.Notifier) {
	t.Helper()
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
		defer stopCancel()
		_ = notifier.Stop(stopCtx)
	})
}

func receiveString(t *testing.T, ch <-chan string, label string) string {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for %s", label)
		return ""
	}
}

func waitForTestChannel(t *testing.T, ch <-chan struct{}, timeout time.Duration, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(timeout):
		t.Fatal(label)
	}
}
