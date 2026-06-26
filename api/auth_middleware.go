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

func loginRedirectPath() string {
	webPath, err := (&service.SettingService{}).GetWebPath()
	if err != nil || webPath == "" {
		return "/login"
	}
	return strings.TrimRight(webPath, "/") + "/login"
}
