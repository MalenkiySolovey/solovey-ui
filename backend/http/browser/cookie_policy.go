package browser

import (
	"net/http"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/backend/http/requestidentity"
	"github.com/gin-gonic/gin"
)

func (b *Service) ResolveCookieSecure(c *gin.Context) bool {
	if b.Settings != nil {
		forceSecure, err := b.Settings.GetForceCookieSecure()
		if err != nil {
			b.warning("invalid forceCookieSecure setting:", err)
		} else if forceSecure {
			return true
		}

		if webURI, err := b.Settings.GetWebURI(); err == nil {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(webURI)), "https://") {
				return true
			}
		} else {
			b.warning("unable to get webURI:", err)
		}

		if webDomain, err := b.Settings.GetWebDomain(); err == nil {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(webDomain)), "https://") {
				return true
			}
		} else {
			b.warning("unable to get webDomain:", err)
		}
	}
	return requestidentity.IsHTTPS(c)
}

// ResolveCookieSameSite returns the SameSite mode for session cookies. It is
// Lax by default and Strict when the sessionSameSiteStrict setting is enabled.
func (b *Service) ResolveCookieSameSite() http.SameSite {
	if b.Settings != nil {
		strict, err := b.Settings.GetSessionSameSiteStrict()
		if err != nil {
			b.warning("invalid sessionSameSiteStrict setting:", err)
		} else if strict {
			return http.SameSiteStrictMode
		}
	}
	return http.SameSiteLaxMode
}
