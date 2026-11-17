package api

import (
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

func resolveCookieSecure(c *gin.Context, settings *service.SettingService) bool {
	return browserSecurity(settings).ResolveCookieSecure(c)
}
func resolveCookieSameSite(settings *service.SettingService) http.SameSite {
	return browserSecurity(settings).ResolveCookieSameSite()
}
