package api

import (
	"net/http"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

func checkLogin(c *gin.Context) {
	if !IsLogin(c) {
		if c.GetHeader("X-Requested-With") == "XMLHttpRequest" {
			pureJsonMsg(c, false, "Invalid login")
		} else {
			c.Redirect(http.StatusTemporaryRedirect, loginRedirectPath())
		}
		c.Abort()
	} else {
		c.Next()
	}
}

func restrictPreauthSession(c *gin.Context) {
	securityContext, ok := GetSessionSecurityContext(c)
	if !ok || securityContext.AuthState == service.AuthStateAuthenticated {
		c.Next()
		return
	}
	allowed := false
	switch securityContext.AuthState {
	case service.AuthStatePasswordReset:
		allowed = c.Request.Method == http.MethodGet && strings.HasSuffix(c.Request.URL.Path, "/api/v1/security/posture") ||
			c.Request.Method == http.MethodPost && strings.HasSuffix(c.Request.URL.Path, "/api/v1/security/password/transition")
	case service.AuthStateMFAPending:
		allowed = c.Request.Method == http.MethodGet && strings.HasSuffix(c.Request.URL.Path, "/api/v1/security/posture") ||
			c.Request.Method == http.MethodPost && (strings.HasSuffix(c.Request.URL.Path, "/api/v1/security/mfa/challenge") ||
				strings.HasSuffix(c.Request.URL.Path, "/api/v1/security/mfa/recovery"))
	case service.AuthStateMFARecovery:
		allowed = c.Request.Method == http.MethodGet && strings.HasSuffix(c.Request.URL.Path, "/api/v1/security/posture") ||
			c.Request.Method == http.MethodPost && strings.HasSuffix(c.Request.URL.Path, "/api/v1/security/mfa/recovery/complete")
	}
	allowed = allowed ||
		c.Request.Method == http.MethodGet && strings.HasSuffix(c.Request.URL.Path, "/api/csrf") ||
		c.Request.Method == http.MethodPost && strings.HasSuffix(c.Request.URL.Path, "/api/logout")
	if allowed {
		c.Next()
		return
	}
	c.AbortWithStatusJSON(http.StatusForbidden, Msg{
		Success: false,
		Msg:     "Authentication transition required",
		Obj: gin.H{
			"state": securityContext.AuthState,
		},
	})
}

func loginRedirectPath() string {
	webPath, err := (&service.SettingService{}).GetWebPath()
	if err != nil || webPath == "" {
		return "/login"
	}
	return strings.TrimRight(webPath, "/") + "/login"
}
