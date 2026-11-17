package api

import (
	"github.com/MalenkiySolovey/solovey-backend/http/browser"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"strings"
)

const (
	csrfTokenKey   = browser.CSRFTokenKey
	csrfExpiresKey = browser.CSRFExpiresKey
	csrfHeader     = browser.CSRFHeader
	csrfTTL        = browser.CSRFTokenTTL
)

func (a *ApiService) IssueCSRFToken(c *gin.Context) {
	token, err := browserSecurity(&a.SettingService).IssueCSRFToken(c)
	if err != nil {
		jsonMsg(c, "csrf", err)
		return
	}
	jsonObj(c, token, nil)
}
func (a *ApiService) GetCSRF(c *gin.Context)        { a.IssueCSRFToken(c) }
func ResetSessionCSRF(s sessions.Session)           { browser.ResetSessionCSRF(s) }
func (a *APIHandler) csrfMiddleware(c *gin.Context) { browser.CSRFMiddleware(c) }
func csrfProtectedMethod(method string) bool        { return browser.CSRFProtectedMethod(method) }
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
