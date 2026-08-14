package api

import (
	"encoding/gob"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func init() {
	gob.Register(model.User{})
}

func SetLoginUser(c *gin.Context, userName string, maxAge int, sessionGeneration string) error {
	var user model.User
	if err := dbsqlite.DB().Model(&model.User{}).Where("username = ?", userName).First(&user).Error; err != nil {
		return err
	}
	lifetimePosture := service.LifetimePostureLegacyUnbounded
	if maxAge > 0 {
		lifetimePosture = service.LifetimePostureLegacyExplicit
	}
	return SetLoginSecurity(c, service.LoginSessionSpec{
		UserID:               user.Id,
		Username:             user.Username,
		AuthState:            service.AuthStateAuthenticated,
		Assurance:            service.AssurancePassword,
		LifetimePosture:      lifetimePosture,
		SessionGeneration:    sessionGeneration,
		CredentialGeneration: nonzeroSessionGeneration(user.CredentialGeneration),
		MFAGeneration:        nonzeroSessionGeneration(user.MFAGeneration),
		ClientProvenance:     "direct",
		ClientPrefix:         getRemoteIp(c),
		UserAgentHash:        service.UserAgentDigest(c.Request.UserAgent()),
		DeviceLabel:          boundedDeviceLabel(c.Request.UserAgent()),
		LegacyMaxAge:         time.Duration(maxAge) * time.Minute,
	})
}

func SetLoginSecurity(c *gin.Context, spec service.LoginSessionSpec) error {
	now := spec.Now
	if now.IsZero() {
		now = time.Now()
	}
	// Persist only the request-scoped, proxy-aware identity facts. In
	// particular, inventory receives a privacy prefix (/24 or /56), never the
	// full client address supplied by a caller.
	clientIdentity := RequestClientIdentity(c)
	spec.ClientProvenance = clientIdentity.Provenance
	spec.ClientPrefix = clientIdentity.ClientPrefix
	if spec.AuthState == "" {
		spec.AuthState = service.AuthStateAuthenticated
	}
	if spec.Assurance == "" {
		spec.Assurance = service.AssurancePassword
	}
	if spec.LifetimePosture == "" {
		spec.LifetimePosture = service.LifetimePostureBoundedV1
	}
	if spec.LastMFAAt <= 0 && (spec.Assurance == service.AssuranceMFA || spec.Assurance == service.AssuranceRecovery) {
		spec.LastMFAAt = now.Unix()
	}
	idleDuration := service.DefaultSessionIdle
	absoluteDuration := service.DefaultSessionAbsolute
	cookieMaxAge := 0
	if spec.LifetimePosture == service.LifetimePostureLegacyUnbounded {
		idleDuration = 0
		absoluteDuration = 0
	} else if spec.LifetimePosture == service.LifetimePostureLegacyExplicit && spec.LegacyMaxAge > 0 {
		idleDuration = spec.LegacyMaxAge
		absoluteDuration = spec.LegacyMaxAge
		cookieMaxAge = int(spec.LegacyMaxAge / time.Second)
	}
	if spec.Remembered {
		absoluteDuration = service.DefaultRememberedSession
		cookieMaxAge = int(service.DefaultRememberedSession / time.Second)
	}
	switch spec.AuthState {
	case service.AuthStatePasswordReset:
		idleDuration = service.PasswordResetSessionTTL
		absoluteDuration = service.PasswordResetSessionTTL
		cookieMaxAge = 0
	case service.AuthStateMFAPending, service.AuthStateMFARecovery:
		idleDuration = service.MFAPreauthSessionTTL
		absoluteDuration = service.MFAPreauthSessionTTL
		cookieMaxAge = 0
	}

	options := sessions.Options{
		Path:     "/",
		Secure:   resolveCookieSecure(c, &service.SettingService{}),
		HttpOnly: true,
		SameSite: resolveCookieSameSite(&service.SettingService{}),
		MaxAge:   cookieMaxAge,
	}
	sessionRef, err := common.SecureRandom(32)
	if err != nil {
		return common.NewError("Unable to establish session")
	}
	sessionGenerationRevision := service.SessionGenerationRevision(spec.SessionGeneration)
	if sessionGenerationRevision == "" {
		// Historical installations may intentionally retain an empty global
		// generation until the first guarded security transition. New sessions
		// still need an immutable per-session revision for exact step-up and
		// realtime binding during that compatibility window.
		revision, err := common.SecureRandom(32)
		if err != nil {
			return common.NewError("Unable to establish session")
		}
		sessionGenerationRevision = service.SessionGenerationRevision(revision)
	}

	s := sessions.Default(c)
	s.Set(service.SessionLoginUserKey, spec.Username)
	if spec.SessionGeneration != "" {
		s.Set(service.SessionGenerationKey, spec.SessionGeneration)
	}
	s.Set(service.SessionUserIDKey, uint64(spec.UserID))
	s.Set(service.SessionRefKey, sessionRef)
	s.Set(service.SessionAuthStateKey, spec.AuthState)
	s.Set(service.SessionAssuranceKey, spec.Assurance)
	s.Set(service.SessionLastMFAAtKey, spec.LastMFAAt)
	s.Set(service.SessionLifetimePostureKey, spec.LifetimePosture)
	s.Set(service.SessionCredentialGenerationKey, spec.CredentialGeneration)
	s.Set(service.SessionMFAGenerationKey, spec.MFAGeneration)
	s.Set(service.SessionCreatedAtKey, now.Unix())
	s.Set(service.SessionAuthenticatedAtKey, now.Unix())
	s.Set(service.SessionLastSeenAtKey, now.Unix())
	idleExpiresAt := int64(0)
	if idleDuration > 0 {
		idleExpiresAt = now.Add(idleDuration).Unix()
	}
	absoluteExpiresAt := int64(0)
	if absoluteDuration > 0 {
		absoluteExpiresAt = now.Add(absoluteDuration).Unix()
	}
	s.Set(service.SessionIdleExpiresAtKey, idleExpiresAt)
	s.Set(service.SessionAbsoluteExpiresAtKey, absoluteExpiresAt)
	rememberedExpiresAt := int64(0)
	if spec.Remembered {
		rememberedExpiresAt = now.Add(service.DefaultRememberedSession).Unix()
	}
	s.Set(service.SessionRememberedExpiresAtKey, rememberedExpiresAt)
	s.Set(service.SessionClientProvenanceKey, spec.ClientProvenance)
	s.Set(service.SessionClientPrefixKey, spec.ClientPrefix)
	s.Set(service.SessionUserAgentHashKey, spec.UserAgentHash)
	s.Set(service.SessionDeviceLabelKey, spec.DeviceLabel)
	s.Set(service.SessionGenerationRevisionKey, sessionGenerationRevision)
	if spec.PreAuthChallengeRevision != "" {
		s.Set(service.SessionPreAuthChallengeRevision, spec.PreAuthChallengeRevision)
	}
	ResetSessionCSRF(s)
	// Rotate the session ID on login so a planted pre-auth (CSRF) session cannot
	// survive authentication under an attacker-known ID (session-fixation defense).
	s.Set(service.SessionRegenerateKey, true)
	s.Options(options)

	if err := s.Save(); err != nil {
		logger.Warning("failed to establish server-side session:", err)
		return common.NewError("Unable to establish session")
	}
	return nil
}

func GetLoginUser(c *gin.Context) string {
	s := sessions.Default(c)
	obj := s.Get(service.SessionLoginUserKey)
	if obj == nil {
		return ""
	}
	objStr, ok := obj.(string)
	if !ok {
		return ""
	}
	if !sessionGenerationValid(s) {
		return ""
	}
	if !sessionUserValid(s, objStr) {
		return ""
	}
	return objStr
}

func sessionUserValid(s sessions.Session, username string) bool {
	var user model.User
	err := dbsqlite.DB().Model(&model.User{}).Where("username = ?", username).First(&user).Error
	if err != nil {
		logger.Warning("unable to validate session user:", err)
		return false
	}
	credentialGeneration, credentialOK := sessionUint64(s.Get(service.SessionCredentialGenerationKey))
	mfaGeneration, mfaOK := sessionUint64(s.Get(service.SessionMFAGenerationKey))
	if !credentialOK || !mfaOK {
		// Pre-1.8 sessions are compatible only while the administrator remains at
		// the untouched migration baseline. Credential/MFA transitions increment
		// their durable generations, so even a later global-generation write
		// failure cannot leave a historical session authorized.
		if user.ForcePasswordReset ||
			nonzeroSessionGeneration(user.CredentialGeneration) != 1 ||
			nonzeroSessionGeneration(user.MFAGeneration) != 1 {
			return false
		}
	}
	if credentialOK && credentialGeneration != nonzeroSessionGeneration(user.CredentialGeneration) {
		return false
	}
	if mfaOK && mfaGeneration != nonzeroSessionGeneration(user.MFAGeneration) {
		return false
	}
	if ref, ok := s.Get(service.SessionRefKey).(string); ok && ref != "" && credentialOK && mfaOK {
		_, validationErr := (&service.SecuritySessionService{}).Validate(ref, user.Id, credentialGeneration, mfaGeneration)
		if validationErr != nil && !dbsqlite.IsNotFound(validationErr) {
			// CookieStore is retained only in unit/integration fixtures. The
			// production SQLite store always creates the metadata row.
			var count int64
			if countErr := dbsqlite.DB().Model(&model.SecuritySession{}).Where("ref = ?", ref).Count(&count).Error; countErr != nil || count > 0 {
				return false
			}
		}
	}
	return true
}

func sessionGenerationValid(s sessions.Session) bool {
	current, err := (&service.SettingService{}).GetSessionGeneration()
	if err != nil {
		logger.Warning("unable to get session generation:", err)
		return false
	}
	if current == "" {
		return true
	}
	obj := s.Get(service.SessionGenerationKey)
	sessionGeneration, ok := obj.(string)
	return ok && sessionGeneration == current
}

type SessionSecurityContext struct {
	UserID                    uint
	Username                  string
	Ref                       string
	AuthState                 string
	Assurance                 string
	LastMFAAt                 int64
	SessionGenerationRevision string
	CredentialGeneration      uint64
	MFAGeneration             uint64
}

func GetSessionSecurityContext(c *gin.Context) (SessionSecurityContext, bool) {
	s := sessions.Default(c)
	username := GetLoginUser(c)
	if username == "" {
		return SessionSecurityContext{}, false
	}
	userID, userOK := sessionUint64(s.Get(service.SessionUserIDKey))
	credentialGeneration, credentialOK := sessionUint64(s.Get(service.SessionCredentialGenerationKey))
	mfaGeneration, mfaOK := sessionUint64(s.Get(service.SessionMFAGenerationKey))
	ref, refOK := s.Get(service.SessionRefKey).(string)
	authState, stateOK := s.Get(service.SessionAuthStateKey).(string)
	assurance, assuranceOK := s.Get(service.SessionAssuranceKey).(string)
	lastMFAAt, _ := sessionUint64(s.Get(service.SessionLastMFAAtKey))
	revision, _ := s.Get(service.SessionGenerationRevisionKey).(string)
	if !userOK || !credentialOK || !mfaOK || !refOK || !stateOK || !assuranceOK {
		return SessionSecurityContext{}, false
	}
	return SessionSecurityContext{
		UserID:                    uint(userID),
		Username:                  username,
		Ref:                       ref,
		AuthState:                 authState,
		Assurance:                 assurance,
		LastMFAAt:                 int64(lastMFAAt),
		SessionGenerationRevision: revision,
		CredentialGeneration:      credentialGeneration,
		MFAGeneration:             mfaGeneration,
	}, true
}

func sessionUint64(value any) (uint64, bool) {
	switch typed := value.(type) {
	case uint64:
		return typed, true
	case uint:
		return uint64(typed), true
	case int:
		if typed >= 0 {
			return uint64(typed), true
		}
	case int64:
		if typed >= 0 {
			return uint64(typed), true
		}
	}
	return 0, false
}

func nonzeroSessionGeneration(value uint64) uint64 {
	if value == 0 {
		return 1
	}
	return value
}

func boundedDeviceLabel(userAgent string) string {
	const max = 96
	if len(userAgent) <= max {
		return userAgent
	}
	return userAgent[:max]
}

func IsLogin(c *gin.Context) bool {
	return GetLoginUser(c) != ""
}

func ClearSession(c *gin.Context) {
	s := sessions.Default(c)
	s.Clear()
	s.Options(sessions.Options{
		Path:     "/",
		MaxAge:   -1,
		Secure:   resolveCookieSecure(c, &service.SettingService{}),
		HttpOnly: true,
		SameSite: resolveCookieSameSite(&service.SettingService{}),
	})
	if err := s.Save(); err != nil {
		logger.Warning("failed to save cleared session: ", err)
	}
}
