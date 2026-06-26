package auth

import (
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *Handler) LoadTokens() ([]byte, error) {
	return a.UserService.LoadTokens()
}

func (a *Handler) GetTokens(c *gin.Context) {
	loginUser := a.LoginUser(c)
	tokens, err := a.UserService.GetUserTokens(loginUser)
	a.JSONObj(c, tokens, err)
}

func (a *Handler) AddToken(c *gin.Context) {
	loginUser := a.LoginUser(c)
	expiry := c.Request.FormValue("expiry")
	expiryInt, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil {
		a.JSONMsg(c, "", err)
		return
	}
	desc := c.Request.FormValue("desc")
	scope := c.DefaultPostForm("scope", "admin")
	token, err := a.UserService.AddToken(loginUser, expiryInt, desc, scope)
	if err == nil {
		a.Audit(c, loginUser, "api_token_created", "api_token", service.AuditSeverityWarn, map[string]any{
			"desc":   desc,
			"expiry": expiryInt,
			"scope":  scope,
		})
	}
	a.JSONObj(c, token, err)
}

func (a *Handler) DeleteToken(c *gin.Context) {
	tokenId := c.Request.FormValue("id")
	err := a.UserService.DeleteToken(tokenId)
	if err == nil {
		a.Audit(c, a.LoginUser(c), "api_token_deleted", "api_token", service.AuditSeverityWarn, map[string]any{
			"id": tokenId,
		})
	}
	a.JSONMsg(c, "", err)
}

func (a *Handler) SetTokenEnabled(c *gin.Context) {
	id := c.Request.FormValue("id")
	enabled, err := strconv.ParseBool(c.Request.FormValue("enabled"))
	if err != nil {
		a.JSONMsg(c, "", err)
		return
	}
	err = a.UserService.SetTokenEnabled(id, enabled)
	if err == nil {
		a.Audit(c, a.LoginUser(c), "api_token_enabled_changed", "api_token", service.AuditSeverityWarn, map[string]any{
			"id":      id,
			"enabled": enabled,
		})
	}
	a.JSONMsg(c, "save", err)
}
