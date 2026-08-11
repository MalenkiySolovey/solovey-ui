package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func TestLegacyUnboundedSessionHasNoServerOrCookieExpiry(t *testing.T) {
	settingService := initSessionTestDB(t)
	var admin model.User
	if err := dbsqlite.DB().Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&admin).Update("force_password_reset", false).Error; err != nil {
		t.Fatal(err)
	}
	admin.ForcePasswordReset = false
	generation, err := settingService.GetSessionGeneration()
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("s-ui", cookie.NewStore([]byte("test-secret"))))
	router.GET("/login", func(c *gin.Context) {
		err := SetLoginSecurity(c, service.LoginSessionSpec{
			UserID:               admin.Id,
			Username:             admin.Username,
			AuthState:            service.AuthStateAuthenticated,
			Assurance:            service.AssurancePassword,
			LifetimePosture:      service.LifetimePostureLegacyUnbounded,
			SessionGeneration:    generation,
			CredentialGeneration: nonzeroSessionGeneration(admin.CredentialGeneration),
			MFAGeneration:        nonzeroSessionGeneration(admin.MFAGeneration),
			Now:                  time.Unix(1_900_000_000, 0),
		})
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})
	router.GET("/inspect", func(c *gin.Context) {
		s := sessions.Default(c)
		jsonObj(c, gin.H{
			"posture":  s.Get(service.SessionLifetimePostureKey),
			"idle":     s.Get(service.SessionIdleExpiresAtKey),
			"absolute": s.Get(service.SessionAbsoluteExpiresAtKey),
		}, nil)
	})

	login := performSessionRequest(router, "/login")
	if login.Code != http.StatusNoContent {
		t.Fatalf("login returned %d", login.Code)
	}
	cookieValue := findCookieByName(login.Result().Cookies())
	if cookieValue == nil || cookieValue.MaxAge != 0 {
		t.Fatalf("legacy-unbounded cookie=%#v, want session cookie", cookieValue)
	}
	inspect := performSessionRequest(router, "/inspect", login.Result().Cookies()...)
	if inspect.Code != http.StatusOK || !strings.Contains(inspect.Body.String(), `"posture":"legacy_unbounded"`) ||
		!strings.Contains(inspect.Body.String(), `"idle":0`) || !strings.Contains(inspect.Body.String(), `"absolute":0`) {
		t.Fatalf("legacy-unbounded metadata status=%d body=%s", inspect.Code, inspect.Body.String())
	}
}

func TestSecuritySessionCookieFlagsAndMaxAge(t *testing.T) {
	settingService := initSessionTestDB(t)
	t.Setenv("SUI_FORCE_COOKIE_SECURE", "true")
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]string{"sessionMaxAge": "7"})
	if err != nil {
		t.Fatal(err)
	}
	if err := settingService.Save(dbsqlite.DB(), payload); err != nil {
		t.Fatal(err)
	}
	router := newSecuritySessionMaxAgeRouter(t, settingService)

	login := performSessionRequest(router, "/login")
	if login.Code != http.StatusNoContent {
		t.Fatalf("login returned %d", login.Code)
	}
	cookie := findCookieByName(login.Result().Cookies())
	if cookie == nil {
		t.Fatal("login did not set s-ui cookie")
	}
	if !cookie.Secure {
		t.Fatal("session cookie must be Secure when forced")
	}
	if !cookie.HttpOnly {
		t.Fatal("session cookie must be HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie SameSite=%v, want Lax", cookie.SameSite)
	}
	if cookie.MaxAge != 7*60 {
		t.Fatalf("session cookie MaxAge=%d, want %d", cookie.MaxAge, 7*60)
	}
}

func newSecuritySessionMaxAgeRouter(t *testing.T, settingService interface {
	GetSessionGeneration() (string, error)
	GetSessionMaxAge() (int, error)
}) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("s-ui", cookie.NewStore([]byte("test-secret"))))
	router.GET("/login", func(c *gin.Context) {
		generation, err := settingService.GetSessionGeneration()
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		maxAge, err := settingService.GetSessionMaxAge()
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		if err := SetLoginUser(c, "admin", maxAge, generation); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})
	return router
}

func TestSecuritySessionRotationInvalidatesOldCookie(t *testing.T) {
	settingService := initSessionTestDB(t)
	router := newSessionTestRouter(t, settingService)

	login := performSessionRequest(router, "/login")
	if login.Code != http.StatusNoContent {
		t.Fatalf("login returned %d", login.Code)
	}
	if before := performSessionRequest(router, "/protected", login.Result().Cookies()...); before.Code != http.StatusNoContent {
		t.Fatalf("session should be valid before rotation, got %d", before.Code)
	}
	if _, err := settingService.RotateSessionGeneration(); err != nil {
		t.Fatal(err)
	}
	if after := performSessionRequest(router, "/protected", login.Result().Cookies()...); after.Code != http.StatusUnauthorized {
		t.Fatalf("old session should be unauthorized after rotation, got %d", after.Code)
	}
}

func TestSecurityTransitionRotationInvalidatesPreMetadataLegacySession(t *testing.T) {
	settingService := initSessionTestDB(t)
	var admin model.User
	if err := dbsqlite.DB().Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&admin).Update("force_password_reset", false).Error; err != nil {
		t.Fatal(err)
	}
	admin.ForcePasswordReset = false
	generation, err := settingService.GetSessionGeneration()
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("s-ui", cookie.NewStore([]byte("test-secret"))))
	router.GET("/legacy", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set(service.SessionLoginUserKey, admin.Username)
		session.Set(service.SessionGenerationKey, generation)
		if err := session.Save(); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})
	router.GET("/current", func(c *gin.Context) {
		if err := SetLoginSecurity(c, service.LoginSessionSpec{
			UserID: admin.Id, Username: admin.Username,
			AuthState: service.AuthStateAuthenticated, Assurance: service.AssurancePassword,
			LifetimePosture: service.LifetimePostureLegacyUnbounded, SessionGeneration: generation,
			CredentialGeneration: nonzeroSessionGeneration(admin.CredentialGeneration),
			MFAGeneration:        nonzeroSessionGeneration(admin.MFAGeneration),
		}); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})
	router.GET("/rotate", func(c *gin.Context) {
		handler := securityHTTP{api: &APIHandler{ApiService: NewApiService()}}
		if err := handler.rotateSessionGenerationAndReissue(c, admin.Id, service.AuthStateAuthenticated, service.AssurancePassword); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})
	router.GET("/protected", func(c *gin.Context) {
		if GetLoginUser(c) == "" {
			c.Status(http.StatusUnauthorized)
			return
		}
		c.Status(http.StatusNoContent)
	})

	legacy := performSessionRequest(router, "/legacy")
	current := performSessionRequest(router, "/current")
	if legacy.Code != http.StatusNoContent || current.Code != http.StatusNoContent {
		t.Fatalf("session setup failed: legacy=%d current=%d", legacy.Code, current.Code)
	}
	if response := performSessionRequest(router, "/protected", legacy.Result().Cookies()...); response.Code != http.StatusNoContent {
		t.Fatalf("legacy session was not initially compatible: %d", response.Code)
	}
	rotate := performSessionRequest(router, "/rotate", current.Result().Cookies()...)
	if rotate.Code != http.StatusNoContent {
		t.Fatalf("security transition rotation failed: %d body=%s", rotate.Code, rotate.Body.String())
	}
	if response := performSessionRequest(router, "/protected", legacy.Result().Cookies()...); response.Code != http.StatusUnauthorized {
		t.Fatalf("legacy session survived security transition: %d", response.Code)
	}
	if response := performSessionRequest(router, "/protected", rotate.Result().Cookies()...); response.Code != http.StatusNoContent {
		t.Fatalf("reissued current session is invalid: %d", response.Code)
	}
}

func TestPreMetadataLegacySessionFailsClosedAfterDurableCredentialGenerationChange(t *testing.T) {
	settingService := initSessionTestDB(t)
	if err := dbsqlite.DB().Model(&model.User{}).Where("username = ?", "admin").Update("force_password_reset", false).Error; err != nil {
		t.Fatal(err)
	}
	generation, err := settingService.GetSessionGeneration()
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("s-ui", cookie.NewStore([]byte("test-secret"))))
	router.GET("/legacy", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set(service.SessionLoginUserKey, "admin")
		session.Set(service.SessionGenerationKey, generation)
		if err := session.Save(); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})
	router.GET("/protected", func(c *gin.Context) {
		if GetLoginUser(c) == "" {
			c.Status(http.StatusUnauthorized)
			return
		}
		c.Status(http.StatusNoContent)
	})

	legacy := performSessionRequest(router, "/legacy")
	if before := performSessionRequest(router, "/protected", legacy.Result().Cookies()...); before.Code != http.StatusNoContent {
		t.Fatalf("baseline legacy session status=%d", before.Code)
	}
	if err := dbsqlite.DB().Model(&model.User{}).Where("username = ?", "admin").
		Update("credential_generation", 2).Error; err != nil {
		t.Fatal(err)
	}
	if after := performSessionRequest(router, "/protected", legacy.Result().Cookies()...); after.Code != http.StatusUnauthorized {
		t.Fatalf("legacy session survived durable credential generation change: %d", after.Code)
	}
}

func TestSecuritySessionStrictSameSite(t *testing.T) {
	settingService := initSessionTestDB(t)
	if _, err := settingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]string{"sessionSameSiteStrict": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if err := settingService.Save(dbsqlite.DB(), payload); err != nil {
		t.Fatal(err)
	}
	router := newSecuritySessionMaxAgeRouter(t, settingService)

	login := performSessionRequest(router, "/login")
	if login.Code != http.StatusNoContent {
		t.Fatalf("login returned %d", login.Code)
	}
	cookie := findCookieByName(login.Result().Cookies())
	if cookie == nil {
		t.Fatal("login did not set s-ui cookie")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie SameSite=%v, want Strict", cookie.SameSite)
	}
}
