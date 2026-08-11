package web

import (
	"net/http"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/service"
	ginsessions "github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func TestSQLiteSessionStorePersistsValidatesAndDebouncesSecurityMetadata(t *testing.T) {
	db := initSQLiteSessionTestDB(t)
	var admin model.User
	if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	store, err := NewSQLiteSessionStore(db, []byte("test-session-secret-32-bytes-long"))
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return now }

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(ginsessions.Sessions("s-ui", store))
	router.GET("/login", func(c *gin.Context) {
		session := ginsessions.Default(c)
		session.Set(service.SessionLoginUserKey, admin.Username)
		session.Set(service.SessionUserIDKey, uint64(admin.Id))
		session.Set(service.SessionRefKey, "opaque-session-reference")
		session.Set(service.SessionAuthStateKey, service.AuthStateAuthenticated)
		session.Set(service.SessionAssuranceKey, service.AssurancePassword)
		session.Set(service.SessionLastMFAAtKey, now.Add(-time.Minute).Unix())
		session.Set(service.SessionLifetimePostureKey, service.LifetimePostureBoundedV1)
		session.Set(service.SessionCredentialGenerationKey, uint64(1))
		session.Set(service.SessionMFAGenerationKey, uint64(1))
		session.Set(service.SessionCreatedAtKey, now.Unix())
		session.Set(service.SessionAuthenticatedAtKey, now.Unix())
		session.Set(service.SessionLastSeenAtKey, now.Unix())
		session.Set(service.SessionIdleExpiresAtKey, now.Add(service.DefaultSessionIdle).Unix())
		session.Set(service.SessionAbsoluteExpiresAtKey, now.Add(service.DefaultSessionAbsolute).Unix())
		session.Set(service.SessionRememberedExpiresAtKey, int64(0))
		session.Set(service.SessionClientProvenanceKey, "direct")
		session.Set(service.SessionClientPrefixKey, "198.51.100.0/24")
		session.Set(service.SessionUserAgentHashKey, service.UserAgentDigest("test-agent"))
		session.Set(service.SessionDeviceLabelKey, "test-agent")
		session.Set(service.SessionGenerationRevisionKey, "generation-revision")
		session.Options(ginsessions.Options{Path: "/", HttpOnly: true})
		if err := session.Save(); err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.Status(http.StatusNoContent)
	})
	router.GET("/read", func(c *gin.Context) {
		if ginsessions.Default(c).Get(service.SessionLoginUserKey) == nil {
			c.Status(http.StatusUnauthorized)
			return
		}
		c.Status(http.StatusNoContent)
	})

	login := performSQLiteSessionRequest(router, "/login")
	if login.Code != http.StatusNoContent {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	var metadata model.SecuritySession
	if err := db.Where("ref = ?", "opaque-session-reference").First(&metadata).Error; err != nil {
		t.Fatal(err)
	}
	if metadata.SessionID == "" || metadata.UserID != admin.Id ||
		metadata.ClientPrefix != "198.51.100.0/24" ||
		metadata.UserAgentHash == "" || metadata.LastMFAAt != now.Add(-time.Minute).Unix() {
		t.Fatalf("incomplete security metadata: %#v", metadata)
	}

	now = now.Add(30 * time.Second)
	if response := performSQLiteSessionRequest(router, "/read", cookies...); response.Code != http.StatusNoContent {
		t.Fatalf("read status=%d", response.Code)
	}
	var beforeDebounce model.SecuritySession
	if err := db.Where("ref = ?", metadata.Ref).First(&beforeDebounce).Error; err != nil {
		t.Fatal(err)
	}
	if beforeDebounce.LastSeenAt != metadata.LastSeenAt {
		t.Fatal("last-seen was written inside the one-minute debounce")
	}

	now = now.Add(31 * time.Second)
	if response := performSQLiteSessionRequest(router, "/read", cookies...); response.Code != http.StatusNoContent {
		t.Fatalf("read after debounce status=%d", response.Code)
	}
	var afterDebounce model.SecuritySession
	if err := db.Where("ref = ?", metadata.Ref).First(&afterDebounce).Error; err != nil {
		t.Fatal(err)
	}
	if afterDebounce.LastSeenAt != now.Unix() || afterDebounce.IdleExpiresAt <= beforeDebounce.IdleExpiresAt {
		t.Fatalf("last-seen/idle expiry not advanced: before=%#v after=%#v", beforeDebounce, afterDebounce)
	}

	if err := db.Model(&model.SecuritySession{}).Where("ref = ?", metadata.Ref).
		Updates(map[string]any{
			"state":          service.SessionStateRevoked,
			"revoked_at":     now.Unix(),
			"revoked_reason": "test",
		}).Error; err != nil {
		t.Fatal(err)
	}
	if response := performSQLiteSessionRequest(router, "/read", cookies...); response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session status=%d, want 401", response.Code)
	}
}

func TestSQLiteSessionStoreRotationAtomicallyLinksReplacement(t *testing.T) {
	db := initSQLiteSessionTestDB(t)
	var admin model.User
	if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	store, err := NewSQLiteSessionStore(db, []byte("test-session-secret-32-bytes-long"))
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return now }

	setValues := func(session ginsessions.Session, ref string) {
		session.Set(service.SessionLoginUserKey, admin.Username)
		session.Set(service.SessionUserIDKey, uint64(admin.Id))
		session.Set(service.SessionRefKey, ref)
		session.Set(service.SessionAuthStateKey, service.AuthStateAuthenticated)
		session.Set(service.SessionAssuranceKey, service.AssurancePassword)
		session.Set(service.SessionLifetimePostureKey, service.LifetimePostureBoundedV1)
		session.Set(service.SessionCredentialGenerationKey, nonzeroSessionGeneration(admin.CredentialGeneration))
		session.Set(service.SessionMFAGenerationKey, nonzeroSessionGeneration(admin.MFAGeneration))
		session.Set(service.SessionCreatedAtKey, now.Unix())
		session.Set(service.SessionAuthenticatedAtKey, now.Unix())
		session.Set(service.SessionLastSeenAtKey, now.Unix())
		session.Set(service.SessionIdleExpiresAtKey, now.Add(service.DefaultSessionIdle).Unix())
		session.Set(service.SessionAbsoluteExpiresAtKey, now.Add(service.DefaultSessionAbsolute).Unix())
		session.Set(service.SessionGenerationRevisionKey, "generation-revision")
		session.Options(ginsessions.Options{Path: "/", HttpOnly: true})
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(ginsessions.Sessions("s-ui", store))
	router.GET("/seed", func(c *gin.Context) {
		setValues(ginsessions.Default(c), "old-session-ref")
		if err := ginsessions.Default(c).Save(); err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.Status(http.StatusNoContent)
	})
	router.GET("/rotate", func(c *gin.Context) {
		session := ginsessions.Default(c)
		setValues(session, "new-session-ref")
		session.Set(service.SessionRegenerateKey, true)
		if err := session.Save(); err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
		c.Status(http.StatusNoContent)
	})
	router.GET("/read", func(c *gin.Context) {
		if ginsessions.Default(c).Get(service.SessionLoginUserKey) == nil {
			c.Status(http.StatusUnauthorized)
			return
		}
		c.Status(http.StatusNoContent)
	})

	seed := performSQLiteSessionRequest(router, "/seed")
	if seed.Code != http.StatusNoContent {
		t.Fatalf("seed status=%d body=%s", seed.Code, seed.Body.String())
	}
	oldCookies := seed.Result().Cookies()
	rotate := performSQLiteSessionRequest(router, "/rotate", oldCookies...)
	if rotate.Code != http.StatusNoContent {
		t.Fatalf("rotate status=%d body=%s", rotate.Code, rotate.Body.String())
	}
	newCookies := rotate.Result().Cookies()

	var oldMetadata model.SecuritySession
	if err := db.Where("ref = ?", "old-session-ref").First(&oldMetadata).Error; err != nil {
		t.Fatal(err)
	}
	if oldMetadata.State != service.SessionStateRevoked || oldMetadata.RevokedReason != "session_replaced" ||
		oldMetadata.ReplacementRef != "new-session-ref" || oldMetadata.RevokedAt == 0 {
		t.Fatalf("old session lacks replacement relation: %#v", oldMetadata)
	}
	var newMetadata model.SecuritySession
	if err := db.Where("ref = ?", "new-session-ref").First(&newMetadata).Error; err != nil {
		t.Fatal(err)
	}
	if newMetadata.State != service.SessionStateActive || newMetadata.RevokedAt != 0 || newMetadata.SessionID == oldMetadata.SessionID {
		t.Fatalf("new session metadata is invalid: old=%#v new=%#v", oldMetadata, newMetadata)
	}
	if response := performSQLiteSessionRequest(router, "/read", oldCookies...); response.Code != http.StatusUnauthorized {
		t.Fatalf("replaced cookie status=%d, want 401", response.Code)
	}
	if response := performSQLiteSessionRequest(router, "/read", newCookies...); response.Code != http.StatusNoContent {
		t.Fatalf("replacement cookie status=%d, want 204", response.Code)
	}
}

func TestSQLiteSessionStoreEnforcesIdleAbsoluteAndRememberedExpiryAfterRestart(t *testing.T) {
	tests := []struct {
		name              string
		idleOffset        time.Duration
		absoluteOffset    time.Duration
		rememberedOffset  time.Duration
		advance           time.Duration
		expectedHTTPState int
	}{
		{
			name:              "idle expired",
			idleOffset:        time.Minute,
			absoluteOffset:    time.Hour,
			advance:           2 * time.Minute,
			expectedHTTPState: http.StatusUnauthorized,
		},
		{
			name:              "absolute expired",
			idleOffset:        time.Hour,
			absoluteOffset:    time.Minute,
			advance:           2 * time.Minute,
			expectedHTTPState: http.StatusUnauthorized,
		},
		{
			name:              "remembered expired",
			idleOffset:        time.Hour,
			absoluteOffset:    time.Hour,
			rememberedOffset:  time.Minute,
			advance:           2 * time.Minute,
			expectedHTTPState: http.StatusUnauthorized,
		},
		{
			name:              "all bounds valid",
			idleOffset:        time.Hour,
			absoluteOffset:    2 * time.Hour,
			rememberedOffset:  3 * time.Hour,
			advance:           2 * time.Minute,
			expectedHTTPState: http.StatusNoContent,
		},
		{
			name:              "legacy unbounded remains unbounded after debounce",
			advance:           2 * time.Minute,
			expectedHTTPState: http.StatusNoContent,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := initSQLiteSessionTestDB(t)
			var admin model.User
			if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
				t.Fatal(err)
			}
			now := time.Unix(1_800_000_000, 0)
			key := []byte("test-session-secret-32-bytes-long")
			store, err := NewSQLiteSessionStore(db, key)
			if err != nil {
				t.Fatal(err)
			}
			store.now = func() time.Time { return now }
			gin.SetMode(gin.TestMode)
			loginRouter := gin.New()
			loginRouter.Use(ginsessions.Sessions("s-ui", store))
			loginRouter.GET("/login", func(c *gin.Context) {
				session := ginsessions.Default(c)
				session.Set(service.SessionLoginUserKey, admin.Username)
				session.Set(service.SessionUserIDKey, uint64(admin.Id))
				session.Set(service.SessionRefKey, "restart-expiry-reference")
				session.Set(service.SessionAuthStateKey, service.AuthStateAuthenticated)
				session.Set(service.SessionAssuranceKey, service.AssurancePassword)
				session.Set(service.SessionLifetimePostureKey, service.LifetimePostureBoundedV1)
				session.Set(service.SessionCredentialGenerationKey, uint64(1))
				session.Set(service.SessionMFAGenerationKey, uint64(1))
				session.Set(service.SessionCreatedAtKey, now.Unix())
				session.Set(service.SessionAuthenticatedAtKey, now.Unix())
				session.Set(service.SessionLastSeenAtKey, now.Unix())
				idleExpiresAt := int64(0)
				if test.idleOffset > 0 {
					idleExpiresAt = now.Add(test.idleOffset).Unix()
				}
				absoluteExpiresAt := int64(0)
				if test.absoluteOffset > 0 {
					absoluteExpiresAt = now.Add(test.absoluteOffset).Unix()
				}
				session.Set(service.SessionIdleExpiresAtKey, idleExpiresAt)
				session.Set(service.SessionAbsoluteExpiresAtKey, absoluteExpiresAt)
				remembered := int64(0)
				if test.rememberedOffset > 0 {
					remembered = now.Add(test.rememberedOffset).Unix()
				}
				session.Set(service.SessionRememberedExpiresAtKey, remembered)
				session.Set(service.SessionGenerationRevisionKey, "generation-revision")
				session.Options(ginsessions.Options{Path: "/", HttpOnly: true})
				if err := session.Save(); err != nil {
					c.String(http.StatusInternalServerError, err.Error())
					return
				}
				c.Status(http.StatusNoContent)
			})
			login := performSQLiteSessionRequest(loginRouter, "/login")
			if login.Code != http.StatusNoContent {
				t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
			}

			now = now.Add(test.advance)
			restarted, err := NewSQLiteSessionStore(db, key)
			if err != nil {
				t.Fatal(err)
			}
			restarted.now = func() time.Time { return now }
			readRouter := gin.New()
			readRouter.Use(ginsessions.Sessions("s-ui", restarted))
			readRouter.GET("/read", func(c *gin.Context) {
				if ginsessions.Default(c).Get(service.SessionLoginUserKey) == nil {
					c.Status(http.StatusUnauthorized)
					return
				}
				c.Status(http.StatusNoContent)
			})
			response := performSQLiteSessionRequest(readRouter, "/read", login.Result().Cookies()...)
			if response.Code != test.expectedHTTPState {
				t.Fatalf("read status=%d, want %d", response.Code, test.expectedHTTPState)
			}
			if test.idleOffset == 0 && test.absoluteOffset == 0 {
				var metadata model.SecuritySession
				if err := db.Where("ref = ?", "restart-expiry-reference").First(&metadata).Error; err != nil {
					t.Fatal(err)
				}
				if metadata.IdleExpiresAt != 0 || metadata.AbsoluteExpiresAt != 0 || metadata.LastSeenAt != now.Unix() {
					t.Fatalf("legacy-unbounded session was bounded during touch: %#v", metadata)
				}
			}
		})
	}
}
