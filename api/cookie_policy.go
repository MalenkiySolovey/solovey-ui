package api

import (
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func resolveCookieSecure(c *gin.Context, settings *service.SettingService) bool {
	return browserSecurity(settings).ResolveCookieSecure(c)
}
