//go:build !minimal

package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	telegramservice "github.com/MalenkiySolovey/solovey-ui/components/telegram/service"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

type telegramAuditRecorder struct {
	mu     sync.Mutex
	events []service.AuditEvent
	err    error
}

func (a *telegramAuditRecorder) Record(event service.AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
	return a.err
}

type telegramPortRoundTrip func(*http.Request) (*http.Response, error)

func (f telegramPortRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type telegramPortSettings struct{ telegramsettings.Reader }

func (telegramPortSettings) GetTelegramEnabled() (bool, error) { return true, nil }
func (telegramPortSettings) GetTelegramBotToken(context.Context) (string, error) {
	return "123456:port-private-token", nil
}
func (telegramPortSettings) GetTelegramChatID() (string, error)        { return "-99", nil }
func (telegramPortSettings) GetTelegramTransportMode() (string, error) { return "outbound", nil }
func (telegramPortSettings) GetTelegramOutboundTag() (string, error)   { return "selected-vpn", nil }

func TestTelegramAPICompositionUsesOnlySelectedTransportAndAuditPorts(t *testing.T) {
	initTelegramComponentTestDB(t)
	audit := &telegramAuditRecorder{}
	status := http.StatusOK
	var calls int
	var capturedText string
	var factoryErr error
	transport := componenthost.OutboundHTTPClientFunc(func(tag string, timeout time.Duration) (*http.Client, error) {
		if tag != "selected-vpn" || timeout != 10*time.Second {
			t.Fatalf("transport selection changed: %q %s", tag, timeout)
		}
		if factoryErr != nil {
			return nil, factoryErr
		}
		return &http.Client{Transport: telegramPortRoundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
				t.Fatal("wrong transport operation")
			}
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			capturedText = payload["text"]
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`))}, nil
		})}, nil
	})
	host := componenthost.APIDeps{OutboundHTTP: transport, Audit: componenthost.AuditDeps{Recorder: audit}}
	if host.Runtime != nil {
		t.Fatal("fixture must not receive broad Runtime")
	}
	deps := telegramDeps(host)
	deps.Telegram.Settings = telegramPortSettings{}
	if result := deps.Telegram.SendContext(t.Context(), "Authorization: Bearer secret.jwt.value"); !result.Success || calls != 1 {
		t.Fatalf("narrow send failed: %+v calls=%d", result, calls)
	}
	if strings.Contains(capturedText, "secret.jwt.value") || !strings.Contains(capturedText, "[REDACTED]") {
		t.Fatal("narrow transport received unredacted notification")
	}
	status = http.StatusUnauthorized
	result := deps.Telegram.SendContext(t.Context(), "test")
	data, _ := json.Marshal(result)
	if result.Success || result.ErrorClass != "unauthorized" || strings.Contains(string(data), "port-private-token") || calls != 2 {
		t.Fatalf("safe failed-send projection changed: %s calls=%d", data, calls)
	}
	if deps.AuditService != audit {
		t.Fatal("API did not receive the injected recorder")
	}
	factoryErr = errors.New("selected outbound unavailable: port-private-token")
	result = deps.Telegram.SendContext(t.Context(), "test")
	data, _ = json.Marshal(result)
	if result.Success || result.ErrorClass != "proxy" || calls != 2 || strings.Contains(string(data), "port-private-token") {
		t.Fatal("failed selected outbound fell back or disclosed its error")
	}
}

func TestTelegramNarrowAuditAdaptersPreserveIdentityAndError(t *testing.T) {
	wantErr := errors.New("audit unavailable")
	audit := &telegramAuditRecorder{err: wantErr}
	record := telegramservice.AuditRecord{Actor: "admin", Event: "tg_backup_failed", Resource: "database", Severity: service.AuditSeverityWarn,
		Details: map[string]any{"errorClass": "network"}}
	if err := recordTelegramBackupAudit(audit, record); !errors.Is(err, wantErr) {
		t.Fatal("backup audit error was lost")
	}
	recordTelegramNotifierAudit(audit, "notifier_failed", map[string]any{"errorClass": "network"})
	if len(audit.events) != 2 || audit.events[0].Actor != record.Actor || audit.events[0].Resource != record.Resource ||
		audit.events[0].Event != record.Event || audit.events[0].Severity != record.Severity || audit.events[0].Details["errorClass"] != "network" ||
		audit.events[1].Resource != "notifier" || audit.events[1].Severity != service.AuditSeverityWarn {
		t.Fatalf("audit identity changed: %+v", audit.events)
	}
}

func TestTelegramPassphraseObserverUsesInjectedRecorderAfterCommitWithoutSecret(t *testing.T) {
	initTelegramComponentTestDB(t)
	unregister := registerSettingContribution()
	defer unregister()
	audit := &telegramAuditRecorder{}
	ctx := service.ConfigSaveObserverContext{Object: "settings", LoginUser: "admin",
		Data: json.RawMessage(`{"telegramBackupPassphrase":"private-passphrase-for-port"}`)}
	after, err := telegramBackupPassphraseAuditObserver(audit, ctx)
	if err != nil || after == nil || len(audit.events) != 0 {
		t.Fatalf("before-commit audit: %v", err)
	}
	after()
	data, _ := json.Marshal(audit.events)
	if len(audit.events) != 1 || audit.events[0].Actor != "admin" || audit.events[0].Event != "tg_backup_passphrase_changed" ||
		len(audit.events[0].Details) != 1 || audit.events[0].Details["configured"] != true || strings.Contains(string(data), "private-passphrase-for-port") {
		t.Fatalf("unsafe passphrase audit: %s", data)
	}
	ctx.Data = json.RawMessage(`{"telegramBackupPassphrase":"` + service.StoredSecretMarker + `"}`)
	if after, err := telegramBackupPassphraseAuditObserver(audit, ctx); err != nil || after != nil {
		t.Fatal("stored secret marker became a change")
	}
}

func TestTelegramNarrowPortsPreserveNotifierCancellationAndFailedStopAuthority(t *testing.T) {
	initTelegramComponentTestDB(t)
	started := make(chan struct{})
	canceled := make(chan struct{})
	transport := componenthost.OutboundHTTPClientFunc(func(tag string, timeout time.Duration) (*http.Client, error) {
		if tag != "selected-vpn" || timeout != 10*time.Second {
			t.Errorf("incorrect selected transport: %s %s", tag, timeout)
		}
		return &http.Client{Transport: telegramPortRoundTrip(func(r *http.Request) (*http.Response, error) {
			close(started)
			<-r.Context().Done()
			close(canceled)
			return nil, r.Context().Err()
		})}, nil
	})
	scheduler := &telegramTrackingScheduler{}
	c := &component{}
	t.Cleanup(func() {
		scheduler.stopErr = nil
		if err := c.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := c.Start(t.Context(), lifecycle.Context{Host: componenthost.Deps{Scheduler: scheduler,
		API: componenthost.APIDeps{OutboundHTTP: transport, Audit: componenthost.AuditDeps{Recorder: &telegramAuditRecorder{}}}}}); err != nil {
		t.Fatal(err)
	}
	if err := (&service.SettingService{}).Save(dbsqlite.DB(), json.RawMessage(`{"telegramEnabled":"true","telegramBotToken":"123456:port-private-token","telegramChatID":"-99","telegramTransportMode":"outbound","telegramOutboundTag":"selected-vpn"}`)); err != nil {
		t.Fatal(err)
	}
	notifier := c.notifier
	notifier.Backoff = nil
	notifier.Enqueue(telegramservice.Notification{Event: "port-cancel", Text: "test"})
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("injected transport was not used")
	}
	scheduler.stopErr = errors.New("scheduler has not drained")
	if err := c.Stop(t.Context()); err == nil || !c.started || c.notifier != notifier || c.unregisterEvent == nil || notifier.Stopped() {
		t.Fatal("failed Stop discarded lifecycle authority")
	}
	scheduler.stopErr = nil
	stopCtx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := c.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	default:
		t.Fatal("Stop returned before request cancellation/drain")
	}
	if c.started || c.notifier != nil || !notifier.Stopped() {
		t.Fatal("successful Stop retained runtime state")
	}
}
