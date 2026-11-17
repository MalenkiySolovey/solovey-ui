package api

import (
	"github.com/MalenkiySolovey/solovey-backend/http/browser"
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func browserSecurity(settings *service.SettingService) *browser.Service {
	owner := &browser.Service{LogWarning: logger.Warning}
	if settings != nil {
		owner.Settings = settings
	}
	return owner
}

func SetLoginUser(c *gin.Context, userName string, maxAge int, sessionGeneration string) error {
	return browserSecurity(&service.SettingService{}).SetLoginUser(c, userName, maxAge, sessionGeneration)
}
func SetLoginSecurity(c *gin.Context, spec service.LoginSessionSpec) (service.LoginSessionEstablishment, error) {
	return browserSecurity(&service.SettingService{}).SetLoginSecurity(c, spec)
}
func GetLoginUser(c *gin.Context) string {
	return browserSecurity(&service.SettingService{}).GetLoginUser(c)
}
func IsLogin(c *gin.Context) bool { return GetLoginUser(c) != "" }
func ClearSession(c *gin.Context) { browserSecurity(&service.SettingService{}).ClearSession(c) }

type SessionSecurityContext = browser.SessionSecurityContext

func GetSessionSecurityContext(c *gin.Context) (SessionSecurityContext, bool) {
	return browserSecurity(&service.SettingService{}).GetSessionSecurityContext(c)
}
func sessionUint64(value any) (uint64, bool)       { return browser.SessionUint64(value) }
func nonzeroSessionGeneration(value uint64) uint64 { return browser.NonzeroSessionGeneration(value) }
