package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	panelsecurity "github.com/MalenkiySolovey/solovey-ui/backend/security"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type browserSettings struct {
	generation     string
	secure, strict bool
}

func (s *browserSettings) GetSessionGeneration() (string, error)   { return s.generation, nil }
func (s *browserSettings) GetForceCookieSecure() (bool, error)     { return s.secure, nil }
func (*browserSettings) GetWebURI() (string, error)                { return "", nil }
func (*browserSettings) GetWebDomain() (string, error)             { return "", nil }
func (s *browserSettings) GetSessionSameSiteStrict() (bool, error) { return s.strict, nil }

func TestCSRFRejectsBeforeBrowserMutation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("SUI_TRUSTED_PROXIES", "")
	for _, tc := range []struct {
		name             string
		origins          []string
		expected, actual string
		expired          bool
		status           int
	}{
		{name: "valid", origins: []string{"http://panel.example"}, expected: "accepted-session-token", actual: "accepted-session-token", status: 204},
		{name: "missing origin", expected: "accepted-session-token", actual: "accepted-session-token", status: 403},
		{name: "duplicate origin", origins: []string{"http://panel.example", "http://panel.example"}, expected: "accepted-session-token", actual: "accepted-session-token", status: 403},
		{name: "foreign origin", origins: []string{"http://foreign.example"}, expected: "accepted-session-token", actual: "accepted-session-token", status: 403},
		{name: "malformed origin", origins: []string{"http://panel.example/path"}, expected: "accepted-session-token", actual: "accepted-session-token", status: 403},
		{name: "missing stored token", origins: []string{"http://panel.example"}, actual: "accepted-session-token", status: 403},
		{name: "wrong token", origins: []string{"http://panel.example"}, expected: "accepted-session-token", actual: "other", status: 403},
		{name: "expired token", origins: []string{"http://panel.example"}, expected: "accepted-session-token", actual: "accepted-session-token", expired: true, status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			effects := 0
			router := gin.New()
			router.Use(sessions.Sessions("browser", cookie.NewStore([]byte("browser-security-test-cookie-key"))))
			router.Use(func(c *gin.Context) {
				s := sessions.Default(c)
				s.Set(CSRFTokenKey, tc.expected)
				expiry := time.Now().Add(time.Hour)
				if tc.expired {
					expiry = time.Now().Add(-time.Minute)
				}
				s.Set(CSRFExpiresKey, expiry.Unix())
			})
			router.POST("/mutation", CSRFMiddleware, func(c *gin.Context) { effects++; c.Status(204) })
			req := httptest.NewRequest(http.MethodPost, "http://panel.example/mutation", nil)
			req.RemoteAddr = "198.51.100.10:12345"
			for _, origin := range tc.origins {
				req.Header.Add("Origin", origin)
			}
			req.Header.Set(CSRFHeader, tc.actual)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if tc.status == 403 && effects != 0 {
				t.Fatal("rejected browser request reached mutation owner")
			}
			if tc.status == 204 && effects != 1 {
				t.Fatal("accepted browser request did not reach owner exactly once")
			}
		})
	}
}

func TestBrowserSessionKeepsDurableGenerationAndPrivacyFences(t *testing.T) {
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "sessions.db")
	if err := dbsqlite.InitializeContext(context.Background(), file, dbsqlite.Bootstrap{BeforeOpen: func() error { return nil }, AfterOpen: func(db *gorm.DB) error { return db.AutoMigrate(&model.User{}, &model.SecuritySession{}) }}); err != nil {
		if strings.Contains(err.Error(), "requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := dbsqlite.Close(); err != nil {
			t.Error(err)
		}
	})
	admin := model.User{Username: "admin", CredentialGeneration: 1, MFAGeneration: 1}
	if err := dbsqlite.DB().Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	settings := &browserSettings{generation: "accepted-generation", secure: true, strict: true}
	owner := &Service{Settings: settings}
	router := gin.New()
	router.Use(sessions.Sessions("browser", cookie.NewStore([]byte("browser-security-test-cookie-key"))))
	router.GET("/login", func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set(CSRFTokenKey, "pre-auth-token")
		establishment, err := owner.SetLoginSecurity(c, panelsecurity.LoginSessionSpec{UserID: admin.Id, Username: admin.Username, SessionGeneration: settings.generation, CredentialGeneration: 1, MFAGeneration: 1, ClientPrefix: "must-not-be-persisted", ClientProvenance: "untrusted"})
		if err != nil {
			t.Fatal(err)
		}
		ref, _ := s.Get(panelsecurity.SessionRefKey).(string)
		if len(ref) != 32 || establishment.AuthenticationRevision == "" {
			t.Fatal("session authority was not established")
		}
		if s.Get(panelsecurity.SessionRegenerateKey) != true || s.Get(CSRFTokenKey) != nil {
			t.Fatal("login did not rotate the pre-auth session and CSRF token")
		}
		if s.Get(panelsecurity.SessionClientPrefixKey) != "198.51.100.0/24" || s.Get(panelsecurity.SessionClientProvenanceKey) != "direct" {
			t.Fatal("session persisted caller-supplied identity instead of canonical privacy facts")
		}
		row := model.SecuritySession{SessionID: "durable-session", Ref: ref, UserID: admin.Id, CredentialGeneration: 1, MFAGeneration: 1, State: panelsecurity.SessionStateActive}
		if err := dbsqlite.DB().Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if owner.GetLoginUser(c) != "admin" {
			t.Fatal("accepted session was rejected")
		}
		settings.generation = "next-generation"
		if owner.GetLoginUser(c) != "" {
			t.Fatal("global generation rotation retained browser authority")
		}
		settings.generation = "accepted-generation"
		if err := dbsqlite.DB().Model(&admin).Update("credential_generation", 2).Error; err != nil {
			t.Fatal(err)
		}
		if owner.GetLoginUser(c) != "" {
			t.Fatal("credential transition retained browser authority")
		}
		c.Status(204)
	})
	req := httptest.NewRequest(http.MethodGet, "http://panel.example/login", nil)
	req.RemoteAddr = "198.51.100.10:12345"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != 204 {
		t.Fatalf("login status=%d", response.Code)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("browser cookie policy changed")
	}
}

func TestFailedSessionSaveDoesNotPublishAuthenticationRevision(t *testing.T) {
	var warned bool
	owner := &Service{Settings: &browserSettings{}, LogWarning: func(...any) { warned = true }}
	router := gin.New()
	store := cookie.NewStore([]byte("browser-security-test-cookie-key"))
	router.Use(sessions.Sessions("browser", store))
	router.GET("/login", func(c *gin.Context) {
		sessions.Default(c).Set("oversized-fixture", strings.Repeat("x", 10000))
		result, err := owner.SetLoginSecurity(c, panelsecurity.LoginSessionSpec{Username: "admin", CredentialGeneration: 1, MFAGeneration: 1})
		if err == nil || result.AuthenticationRevision != "" || !warned {
			t.Fatal("failed session save published authority or omitted its diagnostic")
		}
		if err.Error() != "Unable to establish session" {
			t.Fatal("session failure leaked its internal cause")
		}
		c.Status(204)
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://panel.example/login", nil))
}
