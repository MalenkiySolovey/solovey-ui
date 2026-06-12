package api

import (
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) LoadTokens() ([]byte, error) {
	return a.UserService.LoadTokens()
}

func (a *ApiService) GetTokens(c *gin.Context) {
	loginUser := GetLoginUser(c)
	tokens, err := a.UserService.GetUserTokens(loginUser)
	jsonObj(c, tokens, err)
}

func (a *ApiService) AddToken(c *gin.Context) {
	loginUser := GetLoginUser(c)
	expiry := c.Request.FormValue("expiry")
	expiryInt, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	desc := c.Request.FormValue("desc")
	scope := c.DefaultPostForm("scope", "admin")
	token, err := a.UserService.AddToken(loginUser, expiryInt, desc, scope)
	if err == nil {
		a.recordAudit(c, loginUser, "api_token_created", "api_token", service.AuditSeverityWarn, map[string]any{
			"desc":   desc,
			"expiry": expiryInt,
			"scope":  scope,
		})
	}
	jsonObj(c, token, err)
}

func (a *ApiService) DeleteToken(c *gin.Context) {
	tokenId := c.Request.FormValue("id")
	err := a.UserService.DeleteToken(tokenId)
	if err == nil {
		a.recordAudit(c, GetLoginUser(c), "api_token_deleted", "api_token", service.AuditSeverityWarn, map[string]any{
			"id": tokenId,
		})
	}
	jsonMsg(c, "", err)
}

func (a *ApiService) SetTokenEnabled(c *gin.Context) {
	id := c.Request.FormValue("id")
	enabled, err := strconv.ParseBool(c.Request.FormValue("enabled"))
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	err = a.UserService.SetTokenEnabled(id, enabled)
	if err == nil {
		a.recordAudit(c, GetLoginUser(c), "api_token_enabled_changed", "api_token", service.AuditSeverityWarn, map[string]any{
			"id":      id,
			"enabled": enabled,
		})
	}
	jsonMsg(c, "save", err)
}
