package security

import (
	"strings"

	"github.com/gin-gonic/gin"
)

const adminContentSecurityPolicy = "default-src 'self'; base-uri 'self'; object-src 'none'; frame-ancestors 'none'; img-src 'self' data: blob:; font-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self' ws: wss:"

// Admin sets the admin panel's security headers. isSecure reports
// whether the request arrived over HTTPS and gates the HSTS header; callers
// inject a trusted-proxy-aware check (e.g. api.RequestIsHTTPS) so a spoofed
// X-Forwarded-Proto from an untrusted client cannot trigger HSTS. When isSecure
// is nil, only a real TLS connection is treated as secure.
func Admin(isSecure func(*gin.Context) bool) gin.HandlerFunc {
	return adminHeaders("", isSecure)
}

// AdminForBase applies the admin panel's security headers only to the managed
// admin base path. Public surfaces may share the same listener, so a global
// admin middleware would leak the panel's CSP/X-Frame profile onto ordinary
// public pages and make the surface easier to fingerprint.
func AdminForBase(basePath string, isSecure func(*gin.Context) bool) gin.HandlerFunc {
	return adminHeaders(basePath, isSecure)
}

func adminHeaders(basePath string, isSecure func(*gin.Context) bool) gin.HandlerFunc {
	if isSecure == nil {
		isSecure = func(c *gin.Context) bool { return c.Request.TLS != nil }
	}
	return func(c *gin.Context) {
		if basePath == "" || adminPathMatches(basePath, c.Request.URL.Path) {
			h := c.Writer.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Content-Security-Policy", adminContentSecurityPolicy)
			if isSecure(c) {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
		}
		c.Next()
	}
}

func adminPathMatches(basePath, requestPath string) bool {
	if basePath == "" || basePath == "/" {
		return true
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	if !strings.HasSuffix(basePath, "/") {
		basePath += "/"
	}
	trimmedBase := strings.TrimSuffix(basePath, "/")
	return requestPath == trimmedBase || strings.HasPrefix(requestPath, basePath)
}

func Subscriptions() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Cache-Control", "no-store")
		c.Next()
	}
}

func SetPublicSiteHeaders(c *gin.Context) {
	SetPublicSiteHeadersWithCSP(c, "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; img-src 'self' data:; font-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'none'; connect-src 'none'; frame-src 'none'; form-action 'self'")
}

func SetPublicSiteHeadersWithCSP(c *gin.Context, csp string) {
	h := c.Writer.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Del("X-Frame-Options")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Content-Security-Policy", csp)
}
