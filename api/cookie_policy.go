package api

import (
	"net/http"
	"strings"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func resolveCookieSecure(c *gin.Context, settingService *service.SettingService) bool {
	if settingService != nil {
		forceSecure, err := settingService.GetForceCookieSecure()
		if err != nil {
			logger.Warning("invalid forceCookieSecure setting:", err)
		} else if forceSecure {
			return true
		}

		if webURI, err := settingService.GetWebURI(); err == nil {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(webURI)), "https://") {
				return true
			}
		} else {
			logger.Warning("unable to get webURI:", err)
		}

		if webDomain, err := settingService.GetWebDomain(); err == nil {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(webDomain)), "https://") {
				return true
			}
		} else {
			logger.Warning("unable to get webDomain:", err)
		}
	}
	return RequestIsHTTPS(c)
}

// resolveCookieSameSite returns the SameSite mode for session cookies. It is
// Lax by default and Strict when the sessionSameSiteStrict setting is enabled.
func resolveCookieSameSite(settingService *service.SettingService) http.SameSite {
	if settingService != nil {
		strict, err := settingService.GetSessionSameSiteStrict()
		if err != nil {
			logger.Warning("invalid sessionSameSiteStrict setting:", err)
		} else if strict {
			return http.SameSiteStrictMode
		}
	}
	return http.SameSiteLaxMode
}
