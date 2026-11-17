package browser

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/backend/http/requestidentity"
	panelsecurity "github.com/MalenkiySolovey/solovey-ui/backend/security"
	clientidentity "github.com/MalenkiySolovey/solovey-ui/backend/security/clientidentity"
	backendcrypto "github.com/MalenkiySolovey/solovey-ui/backend/security/crypto"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	// #nosec G101 -- session storage key name, not a credential.
	CSRFTokenKey   = "CSRF_TOKEN"
	CSRFExpiresKey = "CSRF_EXPIRES"
	CSRFHeader     = "X-CSRF-Token"
	CSRFTokenTTL   = 2 * time.Hour
)

type CSRFToken struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expiresAt"`
}

func (b *Service) IssueCSRFToken(c *gin.Context) (CSRFToken, error) {
	session := sessions.Default(c)
	now := time.Now()
	token, _ := session.Get(CSRFTokenKey).(string)
	expiresAt, _ := session.Get(CSRFExpiresKey).(int64)
	if token == "" || expiresAt < now.Unix() {
		if sessionToken, ok := SessionBoundCSRFToken(session); ok {
			token = sessionToken
		} else {
			var err error
			token, err = backendcrypto.SecureRandom(32)
			if err != nil {
				return CSRFToken{}, err
			}
		}
		expiresAt = now.Add(CSRFTokenTTL).Unix()
		session.Set(CSRFTokenKey, token)
		session.Set(CSRFExpiresKey, expiresAt)
	}
	session.Options(b.SessionCookieOptions(c, session, now))
	if err := session.Save(); err != nil {
		return CSRFToken{}, err
	}
	return CSRFToken{Token: token, ExpiresAt: expiresAt}, nil
}

func (b *Service) SessionCookieOptions(c *gin.Context, session sessions.Session, now time.Time) sessions.Options {
	options := sessions.Options{
		Path:     "/",
		Secure:   b.ResolveCookieSecure(c),
		HttpOnly: true,
		SameSite: b.ResolveCookieSameSite(),
	}
	if maxAge := CurrentSessionCookieMaxAge(session, now); maxAge > 0 {
		options.MaxAge = maxAge
	}
	return options
}

func SessionBoundCSRFToken(session sessions.Session) (string, bool) {
	if session == nil {
		return "", false
	}
	ref, ok := session.Get(panelsecurity.SessionRefKey).(string)
	ref = strings.TrimSpace(ref)
	if !ok || ref == "" {
		return "", false
	}
	digest := sha256.Sum256([]byte("s-ui-csrf-v1\x00" + ref))
	return hex.EncodeToString(digest[:]), true
}

func CurrentSessionCookieMaxAge(session sessions.Session, now time.Time) int {
	if session == nil {
		return 0
	}
	if rememberedExpiresAt, ok := SessionUint64(session.Get(panelsecurity.SessionRememberedExpiresAtKey)); ok &&
		rememberedExpiresAt > uint64(now.Unix()) {
		return int(rememberedExpiresAt - uint64(now.Unix()))
	}
	posture, _ := session.Get(panelsecurity.SessionLifetimePostureKey).(string)
	if posture != panelsecurity.LifetimePostureLegacyExplicit {
		return 0
	}
	absoluteExpiresAt, ok := SessionUint64(session.Get(panelsecurity.SessionAbsoluteExpiresAtKey))
	if !ok || absoluteExpiresAt <= uint64(now.Unix()) {
		return 0
	}
	return int(absoluteExpiresAt - uint64(now.Unix()))
}

func ResetSessionCSRF(s sessions.Session) {
	s.Delete(CSRFTokenKey)
	s.Delete(CSRFExpiresKey)
}

func CSRFMiddleware(c *gin.Context) {
	if !CSRFProtectedMethod(c.Request.Method) {
		c.Next()
		return
	}
	if allowed, reason := SameOriginRequest(c); !allowed {
		csrfForbidden(c, reason)
		return
	}
	session := sessions.Default(c)
	expected, ok := session.Get(CSRFTokenKey).(string)
	if !ok || expected == "" {
		csrfForbidden(c, "missing csrf session")
		return
	}
	expiresAt, ok := session.Get(CSRFExpiresKey).(int64)
	if !ok || expiresAt < time.Now().Unix() {
		csrfForbidden(c, "expired csrf token")
		return
	}
	actual := c.GetHeader(CSRFHeader)
	if actual == "" || subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		csrfForbidden(c, "invalid csrf token")
		return
	}
	c.Next()
}

func CSRFProtectedMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func SameOriginRequest(c *gin.Context) (bool, string) {
	r := c.Request
	if len(r.Header.Values("Origin")) != 1 {
		return false, "missing or multiple origin"
	}
	originHeader := strings.TrimSpace(r.Header.Get("Origin"))
	if originHeader == "" {
		return false, "missing origin"
	}
	origin, err := url.Parse(originHeader)
	if err != nil || origin.Scheme == "" || origin.Host == "" || origin.User != nil || origin.Path != "" ||
		origin.RawQuery != "" || origin.Fragment != "" {
		return false, "invalid origin"
	}
	identity := requestidentity.FromContext(c)
	if identity.ExternalHost == "" || !identity.ForwardedValid {
		return false, "invalid request authority"
	}
	expectedScheme := identity.DesiredScheme
	if !strings.EqualFold(origin.Scheme, expectedScheme) {
		return false, "origin scheme mismatch"
	}
	if clientidentity.CanonicalOriginAuthority(origin.Scheme, origin.Host) !=
		clientidentity.CanonicalOriginAuthority(expectedScheme, identity.ExternalHost) {
		return false, "origin host mismatch"
	}
	return true, ""
}

func csrfForbidden(c *gin.Context, reason string) {
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"success": false,
		"msg":     "Invalid CSRF token",
		"obj": gin.H{
			"reason": reason,
		},
	})
}
