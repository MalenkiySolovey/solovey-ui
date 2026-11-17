package browser

import (
	"encoding/gob"
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/backend/http/requestidentity"
	panelsecurity "github.com/MalenkiySolovey/solovey-ui/backend/security"
	backendcrypto "github.com/MalenkiySolovey/solovey-ui/backend/security/crypto"
	securitysessions "github.com/MalenkiySolovey/solovey-ui/backend/security/sessions"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func init() {
	gob.Register(model.User{})
}

func (b *Service) SetLoginUser(c *gin.Context, userName string, maxAge int, sessionGeneration string) error {
	var user model.User
	if err := dbsqlite.DB().Model(&model.User{}).Where("username = ?", userName).First(&user).Error; err != nil {
		return err
	}
	lifetimePosture := panelsecurity.LifetimePostureLegacyUnbounded
	if maxAge > 0 {
		lifetimePosture = panelsecurity.LifetimePostureLegacyExplicit
	}
	_, err := b.SetLoginSecurity(c, panelsecurity.LoginSessionSpec{
		UserID:               user.Id,
		Username:             user.Username,
		AuthState:            panelsecurity.AuthStateAuthenticated,
		Assurance:            panelsecurity.AssurancePassword,
		LifetimePosture:      lifetimePosture,
		SessionGeneration:    sessionGeneration,
		CredentialGeneration: NonzeroSessionGeneration(user.CredentialGeneration),
		MFAGeneration:        NonzeroSessionGeneration(user.MFAGeneration),
		ClientProvenance:     "direct",
		ClientPrefix:         requestidentity.FromContext(c).ClientIP,
		UserAgentHash:        panelsecurity.UserAgentDigest(c.Request.UserAgent()),
		DeviceLabel:          BoundedDeviceLabel(c.Request.UserAgent()),
		LegacyMaxAge:         time.Duration(maxAge) * time.Minute,
	})
	return err
}

func (b *Service) SetLoginSecurity(c *gin.Context, spec panelsecurity.LoginSessionSpec) (panelsecurity.LoginSessionEstablishment, error) {
	now := spec.Now
	if now.IsZero() {
		now = time.Now()
	}
	// Persist only the request-scoped, proxy-aware identity facts. In
	// particular, inventory receives a privacy prefix (/24 or /56), never the
	// full client address supplied by a caller.
	clientIdentity := requestidentity.FromContext(c)
	spec.ClientProvenance = clientIdentity.Provenance
	spec.ClientPrefix = clientIdentity.ClientPrefix
	if spec.AuthState == "" {
		spec.AuthState = panelsecurity.AuthStateAuthenticated
	}
	if spec.Assurance == "" {
		spec.Assurance = panelsecurity.AssurancePassword
	}
	if spec.LifetimePosture == "" {
		spec.LifetimePosture = panelsecurity.LifetimePostureBoundedV1
	}
	if spec.LastMFAAt <= 0 && (spec.Assurance == panelsecurity.AssuranceMFA || spec.Assurance == panelsecurity.AssuranceRecovery) {
		spec.LastMFAAt = now.Unix()
	}
	idleDuration := panelsecurity.DefaultSessionIdle
	absoluteDuration := panelsecurity.DefaultSessionAbsolute
	cookieMaxAge := 0
	if spec.LifetimePosture == panelsecurity.LifetimePostureLegacyUnbounded {
		idleDuration = 0
		absoluteDuration = 0
	} else if spec.LifetimePosture == panelsecurity.LifetimePostureLegacyExplicit && spec.LegacyMaxAge > 0 {
		idleDuration = spec.LegacyMaxAge
		absoluteDuration = spec.LegacyMaxAge
		cookieMaxAge = int(spec.LegacyMaxAge / time.Second)
	}
	if spec.Remembered {
		absoluteDuration = panelsecurity.DefaultRememberedSession
		cookieMaxAge = int(panelsecurity.DefaultRememberedSession / time.Second)
	}
	switch spec.AuthState {
	case panelsecurity.AuthStatePasswordReset:
		idleDuration = panelsecurity.PasswordResetSessionTTL
		absoluteDuration = panelsecurity.PasswordResetSessionTTL
		cookieMaxAge = 0
	case panelsecurity.AuthStateMFAPending, panelsecurity.AuthStateMFARecovery:
		idleDuration = panelsecurity.MFAPreauthSessionTTL
		absoluteDuration = panelsecurity.MFAPreauthSessionTTL
		cookieMaxAge = 0
	}

	options := sessions.Options{
		Path:     "/",
		Secure:   b.ResolveCookieSecure(c),
		HttpOnly: true,
		SameSite: b.ResolveCookieSameSite(),
		MaxAge:   cookieMaxAge,
	}
	sessionRef, err := backendcrypto.SecureRandom(32)
	if err != nil {
		return panelsecurity.LoginSessionEstablishment{}, errors.New("Unable to establish session")
	}
	sessionGenerationRevision := panelsecurity.SessionGenerationRevision(spec.SessionGeneration)
	if sessionGenerationRevision == "" {
		// Historical installations may intentionally retain an empty global
		// generation until the first guarded security transition. New sessions
		// still need an immutable per-session revision for exact step-up and
		// realtime binding during that compatibility window.
		revision, err := backendcrypto.SecureRandom(32)
		if err != nil {
			return panelsecurity.LoginSessionEstablishment{}, errors.New("Unable to establish session")
		}
		sessionGenerationRevision = panelsecurity.SessionGenerationRevision(revision)
	}

	s := sessions.Default(c)
	s.Set(panelsecurity.SessionLoginUserKey, spec.Username)
	if spec.SessionGeneration != "" {
		s.Set(panelsecurity.SessionGenerationKey, spec.SessionGeneration)
	}
	s.Set(panelsecurity.SessionUserIDKey, uint64(spec.UserID))
	s.Set(panelsecurity.SessionRefKey, sessionRef)
	s.Set(panelsecurity.SessionAuthStateKey, spec.AuthState)
	s.Set(panelsecurity.SessionAssuranceKey, spec.Assurance)
	s.Set(panelsecurity.SessionLastMFAAtKey, spec.LastMFAAt)
	s.Set(panelsecurity.SessionLifetimePostureKey, spec.LifetimePosture)
	s.Set(panelsecurity.SessionCredentialGenerationKey, spec.CredentialGeneration)
	s.Set(panelsecurity.SessionMFAGenerationKey, spec.MFAGeneration)
	s.Set(panelsecurity.SessionCreatedAtKey, now.Unix())
	s.Set(panelsecurity.SessionAuthenticatedAtKey, now.Unix())
	s.Set(panelsecurity.SessionLastSeenAtKey, now.Unix())
	idleExpiresAt := int64(0)
	if idleDuration > 0 {
		idleExpiresAt = now.Add(idleDuration).Unix()
	}
	absoluteExpiresAt := int64(0)
	if absoluteDuration > 0 {
		absoluteExpiresAt = now.Add(absoluteDuration).Unix()
	}
	s.Set(panelsecurity.SessionIdleExpiresAtKey, idleExpiresAt)
	s.Set(panelsecurity.SessionAbsoluteExpiresAtKey, absoluteExpiresAt)
	rememberedExpiresAt := int64(0)
	if spec.Remembered {
		rememberedExpiresAt = now.Add(panelsecurity.DefaultRememberedSession).Unix()
	}
	s.Set(panelsecurity.SessionRememberedExpiresAtKey, rememberedExpiresAt)
	s.Set(panelsecurity.SessionClientProvenanceKey, spec.ClientProvenance)
	s.Set(panelsecurity.SessionClientPrefixKey, spec.ClientPrefix)
	s.Set(panelsecurity.SessionUserAgentHashKey, spec.UserAgentHash)
	s.Set(panelsecurity.SessionDeviceLabelKey, spec.DeviceLabel)
	s.Set(panelsecurity.SessionGenerationRevisionKey, sessionGenerationRevision)
	if spec.PreAuthChallengeRevision != "" {
		s.Set(panelsecurity.SessionPreAuthChallengeRevision, spec.PreAuthChallengeRevision)
	}
	ResetSessionCSRF(s)
	// Rotate the session ID on login so a planted pre-auth (CSRF) session cannot
	// survive authentication under an attacker-known ID (session-fixation defense).
	s.Set(panelsecurity.SessionRegenerateKey, true)
	s.Options(options)

	if err := s.Save(); err != nil {
		b.warning("failed to establish server-side session:", err)
		return panelsecurity.LoginSessionEstablishment{}, errors.New("Unable to establish session")
	}
	return panelsecurity.LoginSessionEstablishment{
		AuthenticationRevision: panelsecurity.SessionAuthenticationRevision(sessionRef),
	}, nil
}

func (b *Service) GetLoginUser(c *gin.Context) string {
	s := sessions.Default(c)
	obj := s.Get(panelsecurity.SessionLoginUserKey)
	if obj == nil {
		return ""
	}
	objStr, ok := obj.(string)
	if !ok {
		return ""
	}
	if !b.sessionGenerationValid(s) {
		return ""
	}
	if !b.sessionUserValid(s, objStr) {
		return ""
	}
	return objStr
}

func (b *Service) sessionUserValid(s sessions.Session, username string) bool {
	var user model.User
	err := dbsqlite.DB().Model(&model.User{}).Where("username = ?", username).First(&user).Error
	if err != nil {
		b.warning("unable to validate session user:", err)
		return false
	}
	credentialGeneration, credentialOK := SessionUint64(s.Get(panelsecurity.SessionCredentialGenerationKey))
	mfaGeneration, mfaOK := SessionUint64(s.Get(panelsecurity.SessionMFAGenerationKey))
	if !credentialOK || !mfaOK {
		// Pre-1.8 sessions are compatible only while the administrator remains at
		// the untouched migration baseline. Credential/MFA transitions increment
		// their durable generations, so even a later global-generation write
		// failure cannot leave a historical session authorized.
		if user.ForcePasswordReset ||
			NonzeroSessionGeneration(user.CredentialGeneration) != 1 ||
			NonzeroSessionGeneration(user.MFAGeneration) != 1 {
			return false
		}
	}
	if credentialOK && credentialGeneration != NonzeroSessionGeneration(user.CredentialGeneration) {
		return false
	}
	if mfaOK && mfaGeneration != NonzeroSessionGeneration(user.MFAGeneration) {
		return false
	}
	if ref, ok := s.Get(panelsecurity.SessionRefKey).(string); ok && ref != "" && credentialOK && mfaOK {
		if _, err := (&securitysessions.SecuritySessionService{}).Validate(ref, user.Id, credentialGeneration, mfaGeneration); err != nil {
			return false
		}
	}
	return true
}

func (b *Service) sessionGenerationValid(s sessions.Session) bool {
	if b.Settings == nil {
		return false
	}
	current, err := b.Settings.GetSessionGeneration()
	if err != nil {
		b.warning("unable to get session generation:", err)
		return false
	}
	if current == "" {
		return true
	}
	obj := s.Get(panelsecurity.SessionGenerationKey)
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

func (b *Service) GetSessionSecurityContext(c *gin.Context) (SessionSecurityContext, bool) {
	s := sessions.Default(c)
	username := b.GetLoginUser(c)
	if username == "" {
		return SessionSecurityContext{}, false
	}
	userID, userOK := SessionUint64(s.Get(panelsecurity.SessionUserIDKey))
	credentialGeneration, credentialOK := SessionUint64(s.Get(panelsecurity.SessionCredentialGenerationKey))
	mfaGeneration, mfaOK := SessionUint64(s.Get(panelsecurity.SessionMFAGenerationKey))
	ref, refOK := s.Get(panelsecurity.SessionRefKey).(string)
	authState, stateOK := s.Get(panelsecurity.SessionAuthStateKey).(string)
	assurance, assuranceOK := s.Get(panelsecurity.SessionAssuranceKey).(string)
	lastMFAAt, _ := SessionUint64(s.Get(panelsecurity.SessionLastMFAAtKey))
	revision, _ := s.Get(panelsecurity.SessionGenerationRevisionKey).(string)
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

func SessionUint64(value any) (uint64, bool) {
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

func NonzeroSessionGeneration(value uint64) uint64 {
	if value == 0 {
		return 1
	}
	return value
}

func BoundedDeviceLabel(userAgent string) string {
	const max = 96
	if len(userAgent) <= max {
		return userAgent
	}
	return userAgent[:max]
}

func (b *Service) IsLogin(c *gin.Context) bool {
	return b.GetLoginUser(c) != ""
}

func (b *Service) ClearSession(c *gin.Context) {
	s := sessions.Default(c)
	s.Clear()
	s.Options(sessions.Options{
		Path:     "/",
		MaxAge:   -1,
		Secure:   b.ResolveCookieSecure(c),
		HttpOnly: true,
		SameSite: b.ResolveCookieSameSite(),
	})
	if err := s.Save(); err != nil {
		b.warning("failed to save cleared session: ", err)
	}
}
