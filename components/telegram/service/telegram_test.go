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

	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/componentkit/telegram"
	telegramservice "github.com/MalenkiySolovey/solovey-ui/components/telegram/service"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
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

func TestTelegramNotifierRetriesAndAuditsFailure(t *testing.T) {
	type auditRecord struct {
		event   string
		details map[string]any
	}
	auditCh := make(chan auditRecord, 1)
	var attempts atomic.Int32
	notifier := telegramservice.NewNotifier(4, func(string) telegramservice.Result {
		attempts.Add(1)
		return telegramservice.Result{ErrorClass: "network"}
	}, func(event string, details map[string]any) {
		auditCh <- auditRecord{event: event, details: details}
	})
	notifier.Backoff = []time.Duration{time.Millisecond, time.Millisecond}

	notifier.Enqueue(telegramservice.Notification{Event: "login_failed", Text: "message body"})

	var record auditRecord
	select {
	case record = <-auditCh:
	case <-time.After(time.Second):
		t.Fatal("notifier_failed audit was not recorded")
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts.Load())
	}
	if record.event != "notifier_failed" {
		t.Fatalf("unexpected audit event: %s", record.event)
	}
	if record.details["channel"] != "telegram" ||
		record.details["event"] != "login_failed" ||
		record.details["errorClass"] != "network" ||
		record.details["attempts"] != 3 {
		t.Fatalf("unexpected audit details: %#v", record.details)
	}
	if _, ok := record.details["text"]; ok {
		t.Fatalf("message text leaked to audit details: %#v", record.details)
	}
}

func TestTelegramNotifierStopCancelsBackoff(t *testing.T) {
	firstSend := make(chan struct{})
	var attempts atomic.Int32
	var firstOnce sync.Once
	notifier := telegramservice.NewNotifier(1, func(string) telegramservice.Result {
		if attempts.Add(1) == 1 {
			firstOnce.Do(func() { close(firstSend) })
		}
		return telegramservice.Result{ErrorClass: "network"}
	}, nil)
	notifier.Backoff = []time.Duration{time.Hour}

	notifier.Enqueue(telegramservice.Notification{Event: "backoff-cancellation", Text: "message"})
	waitForTestChannel(t, firstSend, time.Second, "first telegram send did not start")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := notifier.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("Stop did not cancel notifier backoff quickly; elapsed=%s", elapsed)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("Stop should prevent a retry after backoff cancellation, got %d sends", got)
	}
}

func TestTelegramNotifierStopCancelsInFlightSend(t *testing.T) {
	sendStarted := make(chan struct{})
	sendCanceled := make(chan struct{})
	notifier := telegramservice.NewNotifierContext(1, func(ctx context.Context, _ string) telegramservice.Result {
		close(sendStarted)
		<-ctx.Done()
		close(sendCanceled)
		return telegramservice.Result{ErrorClass: "network"}
	}, nil)
	notifier.Backoff = nil
	notifier.Enqueue(telegramservice.Notification{Event: "shutdown", Text: "message"})
	waitForTestChannel(t, sendStarted, time.Second, "telegram send did not start")

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := notifier.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	waitForTestChannel(t, sendCanceled, time.Second, "telegram send did not observe lifecycle cancellation")
}

func TestTelegramNotifierStopDiscardsQueuedWork(t *testing.T) {
	firstStarted := make(chan struct{})
	var delivered atomic.Int32
	notifier := telegramservice.NewNotifierContext(4, func(ctx context.Context, text string) telegramservice.Result {
		if delivered.Add(1) == 1 {
			close(firstStarted)
			<-ctx.Done()
		}
		return telegramservice.Result{ErrorClass: "network"}
	}, nil)
	notifier.Backoff = nil
	notifier.Enqueue(telegramservice.Notification{Event: "first", Text: "first"})
	waitForTestChannel(t, firstStarted, time.Second, "first telegram send did not start")
	notifier.Enqueue(telegramservice.Notification{Event: "queued", Text: "queued"})

	if err := notifier.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := delivered.Load(); got != 1 {
		t.Fatalf("Stop delivered queued work after cancellation: sends=%d", got)
	}
	if got := notifier.QueueLen(); got != 0 {
		t.Fatalf("Stop retained %d queued notifications", got)
	}
}

func TestTelegramServiceWithoutSettingsFailsClosed(t *testing.T) {
	result := (&telegramservice.Service{}).TestTelegram()
	if result.Success || result.ErrorClass != "settings" {
		t.Fatalf("nil settings result = %#v, want settings failure", result)
	}
}

func TestTelegramNotifierDropsOldestAndAuditsOverflow(t *testing.T) {
	type auditRecord struct {
		event   string
		details map[string]any
	}
	auditCh := make(chan auditRecord, 1)
	sent := make(chan string, 4)
	releaseFirst := make(chan struct{})
	var blockFirst sync.Once
	notifier := telegramservice.NewNotifier(2, func(text string) telegramservice.Result {
		sent <- text
		blockFirst.Do(func() { <-releaseFirst })
		return telegramservice.Result{Success: true}
	}, func(event string, details map[string]any) {
		auditCh <- auditRecord{event: event, details: details}
	})
	notifier.Backoff = nil

	notifier.Enqueue(telegramservice.Notification{Event: "e1", Text: "e1"})
	if got := receiveString(t, sent, "first send"); got != "e1" {
		t.Fatalf("unexpected first send: %s", got)
	}
	notifier.Enqueue(telegramservice.Notification{Event: "e2", Text: "e2"})
	notifier.Enqueue(telegramservice.Notification{Event: "e3", Text: "e3"})
	notifier.Enqueue(telegramservice.Notification{Event: "e4", Text: "e4"})

	var record auditRecord
	select {
	case record = <-auditCh:
	case <-time.After(time.Second):
		t.Fatal("notifier_overflow audit was not recorded")
	}
	if record.event != "notifier_overflow" {
		t.Fatalf("unexpected audit event: %s", record.event)
	}
	if record.details["channel"] != "telegram" ||
		record.details["droppedEvent"] != "e2" ||
		record.details["queuedEvent"] != "e4" {
		t.Fatalf("unexpected overflow details: %#v", record.details)
	}

	close(releaseFirst)
	if got := receiveString(t, sent, "second send"); got != "e3" {
		t.Fatalf("drop-oldest should keep e3, got %s", got)
	}
	if got := receiveString(t, sent, "third send"); got != "e4" {
		t.Fatalf("drop-oldest should keep e4, got %s", got)
	}
	select {
	case got := <-sent:
		t.Fatalf("dropped event was delivered: %s", got)
	case <-time.After(50 * time.Millisecond):
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

func TestTelegramNotifierStopIsIdempotentAndRejectsLateEnqueue(t *testing.T) {
	sent := make(chan string, 2)
	notifier := telegramservice.NewNotifier(10, func(text string) telegramservice.Result {
		sent <- text
		return telegramservice.Result{Success: true}
	}, nil)
	notifier.Backoff = nil

	notifier.Enqueue(telegramservice.Notification{Event: "before_stop", Text: "before_stop"})
	if got := receiveString(t, sent, "before stop send"); got != "before_stop" {
		t.Fatalf("unexpected first send: %s", got)
	}

	if err := notifier.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := notifier.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !notifier.Stopped() {
		t.Fatal("notifier should be stopped")
	}

	notifier.Enqueue(telegramservice.Notification{Event: "after_stop", Text: "after_stop"})
	select {
	case got := <-sent:
		t.Fatalf("late enqueue delivered after stop: %s", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestTelegramNotifierStopBeforeStartPreventsStart(t *testing.T) {
	notifier := telegramservice.NewNotifier(10, func(string) telegramservice.Result {
		t.Fatal("stopped notifier delivered event")
		return telegramservice.Result{Success: true}
	}, nil)

	if err := notifier.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	notifier.Enqueue(telegramservice.Notification{Event: "after_stop", Text: "after_stop"})

	if !notifier.Stopped() || notifier.Started() || notifier.QueueLen() != 0 {
		t.Fatalf("unexpected notifier state after stop-before-start: started=%v stopped=%v queue=%d", notifier.Started(), notifier.Stopped(), notifier.QueueLen())
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
