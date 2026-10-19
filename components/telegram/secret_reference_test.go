//go:build !minimal

package telegram

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	telegramapi "github.com/MalenkiySolovey/solovey-ui/components/telegram/api"
	telegramsettings "github.com/MalenkiySolovey/solovey-ui/components/telegram/internal/settings"
	telegramservice "github.com/MalenkiySolovey/solovey-ui/components/telegram/service"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	settingscrypto "github.com/MalenkiySolovey/solovey-ui/internal/settings/crypto"
	settingsschema "github.com/MalenkiySolovey/solovey-ui/internal/settings/schema"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type referenceMultipart struct{ *bytes.Reader }

func (referenceMultipart) Close() error { return nil }

type referenceTransport func(*http.Request) (*http.Response, error)

func (f referenceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTelegramStoredSecretReferenceAuthDiscoveryBackupResealAndReplacement(t *testing.T) {
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(registerSettingContribution())
	t.Setenv("SUI_SECRETBOX_KEY", "")
	dbbackup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() {
		dbbackup.SetSendSighupHook(nil)
		if current := dbsqlite.DB(); current != nil {
			if sqlDB, err := current.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
	})
	settings := &service.SettingService{}
	if _, err := settings.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	const fakeA, fakeB = "123456:deterministic-telegram-alpha", "123456:deterministic-telegram-bravo"
	save := func(value string) {
		t.Helper()
		payload, _ := json.Marshal(map[string]string{telegramsettings.BotTokenKey: value})
		if err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error { return settings.Save(tx, payload) }); err != nil {
			t.Fatal(err)
		}
	}
	save(fakeA)
	var original model.Setting
	if err := dbsqlite.DB().Where("key = ?", telegramsettings.BotTokenKey).First(&original).Error; err != nil || !secretbox.IsEncrypted(original.Value) {
		t.Fatal("token not stored with existing crypto")
	}
	providerCalls := 0
	expected := fakeA
	provider := &telegramservice.Service{Settings: telegramsettings.Reader{}, Client: &http.Client{Transport: referenceTransport(func(r *http.Request) (*http.Response, error) {
		providerCalls++
		if !strings.Contains(r.URL.Path, "/bot"+expected+"/getUpdates") {
			t.Error("owner operation used wrong credential snapshot")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true,"result":[{"update_id":1,"message":{"chat":{"id":-99,"type":"supergroup"}}}]}`))}, nil
	})}}
	allowed := false
	engine := gin.New()
	telegramapi.RegisterRoutes(engine.Group("/api/v1"), telegramapi.Deps{Settings: telegramsettings.Reader{}, Telegram: provider,
		RequireScope: func(c *gin.Context, _ string, _ ...string) bool {
			if !allowed {
				c.AbortWithStatus(http.StatusForbidden)
			}
			return allowed
		},
		Actor:          func(*gin.Context) string { return "fixture-admin" },
		RemoteIP:       func(*gin.Context) string { return "127.0.0.1" },
		CheckRateLimit: func(string) (time.Duration, error) { return 0, nil },
		Audit:          func(*gin.Context, string, string, string, string, map[string]any) {},
		JSONObj: func(c *gin.Context, value interface{}, err error) {
			c.JSON(http.StatusOK, telegramapi.Envelope{Success: err == nil, Obj: value})
		}})
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/telegram/detect-chat", strings.NewReader(`{}`))
		req = req.WithContext(t.Context())
		engine.ServeHTTP(r, req)
		return r
	}
	if response := request(); response.Code != http.StatusForbidden || providerCalls != 0 {
		t.Fatal("unauthorized request reached stored credential use")
	}
	var deniedUses int64
	if err := dbsqlite.DB().Model(&model.AuditEvent{}).Where("event = ?", "component_secret_use").Count(&deniedUses).Error; err != nil || deniedUses != 0 {
		t.Fatal("denied route resolved SecretRef")
	}
	allowed = true
	if response := request(); response.Code != 200 || !strings.Contains(response.Body.String(), `"chatId":"-99"`) || strings.Contains(response.Body.String(), fakeA) {
		t.Fatal("stored-token discovery or redaction failed")
	}
	masked, err := settings.GetAllSetting()
	if err != nil || (*masked)[settingsschema.SecretPresenceKey(telegramsettings.BotTokenKey)] != "true" || (*masked)[telegramsettings.BotTokenKey] == fakeA {
		t.Fatal("existing token presence/masking changed")
	}
	backup, err := dbbackup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	// Adopt the accepted environment key, replace the live credential, then
	// restore the older DB-key ciphertext through the actual restore hooks.
	t.Setenv("SUI_SECRETBOX_KEY", base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	save(fakeB)
	expected = fakeB
	if result := provider.DetectChatContext(t.Context(), ""); !result.Success {
		t.Fatal("replacement was not usable")
	}
	if err := dbbackup.RestoreContext(t.Context(), referenceMultipart{bytes.NewReader(backup)}); err != nil {
		t.Fatal(err)
	}
	expected = fakeA
	if result := provider.DetectChatContext(t.Context(), ""); !result.Success {
		t.Fatal("restored token not usable through SecretRef")
	}
	var restored model.Setting
	if err := dbsqlite.DB().Where("key = ?", telegramsettings.BotTokenKey).First(&restored).Error; err != nil || restored.Value == original.Value || !secretbox.IsEncrypted(restored.Value) {
		t.Fatal("restore did not reseal original token")
	}
	codec := settingscrypto.Codec{MasterSecret: settings.GetSecret}
	candidates, err := codec.SecretboxCandidates()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := candidates[0].Box.DecryptString(restored.Value, telegramsettings.BotTokenKey)
	if err != nil || decoded != fakeA {
		t.Fatal("restored token not sealed by preferred environment key")
	}
	var audits []model.AuditEvent
	if err := dbsqlite.DB().Where("event = ?", "component_secret_use").Find(&audits).Error; err != nil || len(audits) < 2 {
		t.Fatal("real owner credential audit missing")
	}
	encoded, _ := json.Marshal(audits)
	if strings.Contains(string(encoded), fakeA) || strings.Contains(string(encoded), fakeB) {
		t.Fatal("secret leaked into audit")
	}
	// Existing owner DropData remains the deletion authority.
	if err := (&component{}).DropData(t.Context(), lifecycle.Context{}); err != nil {
		t.Fatal(err)
	}
	if token, err := (telegramsettings.Reader{}).GetTelegramBotToken(t.Context()); err != nil || token != "" {
		t.Fatal("deleted credential remained present")
	}
}
