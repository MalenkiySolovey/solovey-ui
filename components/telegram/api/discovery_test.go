//go:build !minimal

package telegramapi

import (
	"encoding/json"
	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	telegramservice "github.com/MalenkiySolovey/solovey-ui/components/telegram/service"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const handlerTestToken = "123456:handler-test-token"

type discoveryReader struct{ telegramsettings.Reader }

func (discoveryReader) GetTelegramBotToken() (string, error) { return handlerTestToken, nil }

type handlerRoundTrip func(*http.Request) (*http.Response, error)

func (f handlerRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestDiscoveryRealHandlerScopeInputAuditAndRedaction(t *testing.T) {
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	if err := dbsqlite.Init(filepath.Join(t.TempDir(), "s-ui.db")); err != nil {
		t.Fatal(err)
	}
	db := dbsqlite.DB()
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	var calls atomic.Int32
	var audits []map[string]any
	allowed := true
	expectedToken := handlerTestToken
	provider := &telegramservice.Service{Settings: discoveryReader{}, Client: &http.Client{Transport: handlerRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Path != "/bot"+expectedToken+"/getUpdates" {
			t.Error("incorrect request-local token source")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":[{"update_id":1,"message":{"chat":{"id":-99,"type":"group","title":"private-handler-title"},"text":"private-handler-body"}}]}`))}, nil
	})}}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router.Group("/api"), Deps{Telegram: provider,
		RequireScope: func(c *gin.Context, action string, scopes ...string) bool {
			if action != "telegram" || len(scopes) != 1 || scopes[0] != "admin" {
				t.Error("incorrect discovery scope")
			}
			if !allowed {
				c.AbortWithStatus(403)
			}
			return allowed
		},
		Actor: func(*gin.Context) string { return "admin" }, RemoteIP: func(*gin.Context) string { return "192.0.2.1" }, CheckRateLimit: func(string) (time.Duration, error) { return 0, nil },
		Audit: func(_ *gin.Context, actor, event, resource, severity string, details map[string]any) {
			if actor != "admin" || event != "telegram_chat_discovery" || resource != "telegram" {
				t.Error("incorrect audit identity")
			}
			audits = append(audits, details)
		},
		JSONObj: func(c *gin.Context, obj interface{}, err error) { c.JSON(200, Envelope{Success: err == nil, Obj: obj}) },
	})
	request := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		router.ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/telegram/detect-chat", strings.NewReader(body)))
		return r
	}
	allowed = false
	if r := request(`{"token":"bad"}`); r.Code != 403 || calls.Load() != 0 || len(audits) != 0 {
		t.Fatal("scope must deny before parsing, provider and audit")
	}
	allowed = true
	for _, body := range []string{`{}`, `{"token":"` + service.StoredSecretMarker + `"}`, `{"token":"987654:supplied-handler-test"}`} {
		expectedToken = handlerTestToken
		if strings.Contains(body, "supplied-handler-test") {
			expectedToken = "987654:supplied-handler-test"
		}
		r := request(body)
		if r.Code != 200 {
			t.Fatalf("handler status %d", r.Code)
		}
		var envelope Envelope
		if err := json.Unmarshal(r.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		obj, ok := envelope.Obj.(map[string]any)
		if !envelope.Success || !ok || obj["success"] != true || obj["chatId"] != "-99" {
			t.Fatalf("unexpected safe discovery payload %#v", envelope)
		}
		for _, secret := range []string{expectedToken, "private-handler-title", "private-handler-body"} {
			if strings.Contains(r.Body.String(), secret) {
				t.Fatal("response disclosed private provider material")
			}
		}
		details := audits[len(audits)-1]
		if len(details) != 1 || details["success"] != true {
			t.Fatalf("success audit must contain only its success flag: %#v", details)
		}
	}
	before := calls.Load()
	for _, body := range []string{"", `{"token":42}`, `{"token":"request-only-invalid-secret","extra":"x"}`, `{} {}`, `{"token":"` + strings.Repeat("x", 4096) + `"}`} {
		r := request(body)
		if r.Code != 400 || calls.Load() != before {
			t.Fatalf("invalid bounded body status=%d provider calls=%d", r.Code, calls.Load())
		}
		if strings.Contains(r.Body.String(), "request-only-invalid-secret") {
			t.Fatal("invalid response disclosed input")
		}
		details := audits[len(audits)-1]
		if len(details) != 2 || details["success"] != false || details["errorClass"] != "request" {
			t.Fatalf("invalid body safe audit %#v", details)
		}
	}
}
