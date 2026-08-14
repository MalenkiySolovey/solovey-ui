package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"time"

	clientidentity "github.com/MalenkiySolovey/solovey-ui/internal/httpsecurity/clientidentity"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

const (
	// #nosec G101 -- session storage key name, not a credential.
	csrfTokenKey   = "CSRF_TOKEN"
	csrfExpiresKey = "CSRF_EXPIRES"
	csrfHeader     = "X-CSRF-Token"
	csrfTTL        = 2 * time.Hour
)

func (a *ApiService) IssueCSRFToken(c *gin.Context) {
	session := sessions.Default(c)
	now := time.Now()
	token, _ := session.Get(csrfTokenKey).(string)
	expiresAt, _ := session.Get(csrfExpiresKey).(int64)
	if token == "" || expiresAt < now.Unix() {
		if sessionToken, ok := sessionBoundCSRFToken(session); ok {
			token = sessionToken
		} else {
			var err error
			token, err = common.SecureRandom(32)
			if err != nil {
				jsonMsg(c, "csrf", err)
				return
			}
		}
		expiresAt = now.Add(csrfTTL).Unix()
		session.Set(csrfTokenKey, token)
		session.Set(csrfExpiresKey, expiresAt)
	}
	session.Options(sessionCookieOptions(c, &a.SettingService, session, now))
	if err := session.Save(); err != nil {
		jsonMsg(c, "csrf", err)
		return
	}
	jsonObj(c, gin.H{
		"token":     token,
		"expiresAt": expiresAt,
	}, nil)
}

func sessionCookieOptions(c *gin.Context, settingService *service.SettingService, session sessions.Session, now time.Time) sessions.Options {
	options := sessions.Options{
		Path:     "/",
		Secure:   resolveCookieSecure(c, settingService),
		HttpOnly: true,
		SameSite: resolveCookieSameSite(settingService),
	}
	if maxAge := currentSessionCookieMaxAge(session, now); maxAge > 0 {
		options.MaxAge = maxAge
	}
	return options
}

func sessionBoundCSRFToken(session sessions.Session) (string, bool) {
	if session == nil {
		return "", false
	}
	ref, ok := session.Get(service.SessionRefKey).(string)
	ref = strings.TrimSpace(ref)
	if !ok || ref == "" {
		return "", false
	}
	digest := sha256.Sum256([]byte("s-ui-csrf-v1\x00" + ref))
	return hex.EncodeToString(digest[:]), true
}

func currentSessionCookieMaxAge(session sessions.Session, now time.Time) int {
	if session == nil {
		return 0
	}
	if rememberedExpiresAt, ok := sessionUint64(session.Get(service.SessionRememberedExpiresAtKey)); ok &&
		rememberedExpiresAt > uint64(now.Unix()) {
		return int(rememberedExpiresAt - uint64(now.Unix()))
	}
	posture, _ := session.Get(service.SessionLifetimePostureKey).(string)
	if posture != service.LifetimePostureLegacyExplicit {
		return 0
	}
	absoluteExpiresAt, ok := sessionUint64(session.Get(service.SessionAbsoluteExpiresAtKey))
	if !ok || absoluteExpiresAt <= uint64(now.Unix()) {
		return 0
	}
	return int(absoluteExpiresAt - uint64(now.Unix()))
}

func (a *ApiService) GetCSRF(c *gin.Context) {
	a.IssueCSRFToken(c)
}

func ResetSessionCSRF(s sessions.Session) {
	s.Delete(csrfTokenKey)
	s.Delete(csrfExpiresKey)
}

func (a *APIHandler) csrfMiddleware(c *gin.Context) {
	if !csrfProtectedMethod(c.Request.Method) {
		c.Next()
		return
	}
	if allowed, reason := sameOriginRequest(c); !allowed {
		csrfForbidden(c, reason)
		return
	}
	session := sessions.Default(c)
	expected, ok := session.Get(csrfTokenKey).(string)
	if !ok || expected == "" {
		csrfForbidden(c, "missing csrf session")
		return
	}
	expiresAt, ok := session.Get(csrfExpiresKey).(int64)
	if !ok || expiresAt < time.Now().Unix() {
		csrfForbidden(c, "expired csrf token")
		return
	}
	actual := c.GetHeader(csrfHeader)
	if actual == "" || subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		csrfForbidden(c, "invalid csrf token")
		return
	}
	c.Next()
}

func csrfProtectedMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func (a *APIHandler) cachedCSRFLoginPath() string {
	webPath, err := a.SettingService.GetWebPath()
	if err != nil {
		webPath = "/"
	}
	return csrfLoginPathForBase(webPath)
}

func csrfLoginPathForBase(basePath string) string {
	return joinURL(basePath, "api/login")
}

func csrfExemptPath(path string, loginPath string) bool {
	return false
}

func sameOriginRequest(c *gin.Context) (bool, string) {
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
	identity := RequestClientIdentity(c)
	if identity.ExternalHost == "" || !identity.ForwardedValid {
		return false, "invalid request authority"
	}
	expectedScheme := identity.DesiredScheme
	if !strings.EqualFold(origin.Scheme, expectedScheme) {
		return false, "origin scheme mismatch"
	}
	if clientidentity.CanonicalHostPort(origin.Host) != identity.ExternalHost {
		return false, "origin host mismatch"
	}
	return true, ""
}

func joinURL(base string, child string) string {
	base = strings.TrimSpace(base)
	child = strings.TrimLeft(strings.TrimSpace(child), "/")
	if base == "" {
		base = "/"
	}
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base + child
}

func csrfForbidden(c *gin.Context, reason string) {
	c.AbortWithStatusJSON(http.StatusForbidden, Msg{
		Success: false,
		Msg:     "Invalid CSRF token",
		Obj: gin.H{
			"reason": reason,
		},
	})
}
