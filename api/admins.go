package api

import (
	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) GetUsers(c *gin.Context) {
	users, err := a.UserService.GetUsers()
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	loginUser := GetLoginUser(c)
	result := make([]gin.H, 0, len(*users))
	for _, user := range *users {
		result = append(result, gin.H{
			"id":        user.Id,
			"sortOrder": user.SortOrder,
			"username":  user.Username,
			"lastLogin": user.LastLogins,
			"isCurrent": user.Username == loginUser,
		})
	}
	jsonObj(c, result, nil)
}

func (a *ApiService) ChangePass(c *gin.Context) {
	oldPass := c.Request.FormValue("oldPass")
	newUsername := c.Request.FormValue("newUsername")
	newPass := c.Request.FormValue("newPass")
	// Bind the change to the authenticated session user; never trust a target id
	// from the request, so one admin cannot change another admin's credentials.
	currentUser := GetLoginUser(c)
	err := a.UserService.ChangePass(currentUser, oldPass, newUsername, newPass)
	if err == nil {
		logger.Info("change user credentials success")
		a.recordAudit(c, currentUser, "admin_credentials_changed", "admin", service.AuditSeverityWarn, map[string]any{
			"newUsername": newUsername,
		})
		// Rotate the session generation so every OTHER web session and all WS
		// tokens (including any minted under the old credentials) are invalidated,
		// then re-establish only THIS session under the new generation so the
		// admin who changed the password is not logged out of their own session.
		if newGen, rerr := a.SettingService.RotateSessionGeneration(); rerr != nil {
			logger.Warning("session rotation after credential change failed:", rerr)
		} else {
			sessionMaxAge, _ := a.SettingService.GetSessionMaxAge()
			if serr := SetLoginUser(c, newUsername, sessionMaxAge, newGen); serr != nil {
				logger.Warning("re-establishing session after credential change failed:", serr)
			}
		}
		jsonMsg(c, "save", nil)
	} else {
		logger.Warning("change user credentials failed:", err)
		jsonMsg(c, "", err)
	}
}

func (a *ApiService) AddAdmin(c *gin.Context) {
	loginUser := GetLoginUser(c)
	user, err := a.UserService.AddUser(
		loginUser,
		c.Request.FormValue("currentPass"),
		c.Request.FormValue("username"),
		c.Request.FormValue("password"),
	)
	if err == nil {
		logger.Info("admin user created successfully")
		a.recordAudit(c, loginUser, "admin_created", "admin", service.AuditSeverityWarn, map[string]any{
			"targetUserId": user.Id,
			"username":     user.Username,
		})
		jsonMsgObj(c, "add", gin.H{
			"id":        user.Id,
			"username":  user.Username,
			"lastLogin": user.LastLogins,
			"isCurrent": false,
		}, nil)
	} else {
		logger.Warning("create admin user failed:", err)
		jsonMsg(c, "", err)
	}
}

func (a *ApiService) DeleteAdmin(c *gin.Context) {
	loginUser := GetLoginUser(c)
	result, err := a.UserService.DeleteUser(
		loginUser,
		c.Request.FormValue("currentPass"),
		c.Request.FormValue("id"),
	)
	if err == nil {
		logger.Info("admin user deleted successfully")
		a.recordAudit(c, loginUser, "admin_deleted", "admin", service.AuditSeverityWarn, map[string]any{
			"targetUserId":      result.User.Id,
			"username":          result.User.Username,
			"deletedTokenCount": result.DeletedTokenCount,
		})
		jsonMsg(c, "del", nil)
	} else {
		logger.Warning("delete admin user failed:", err)
		jsonMsg(c, "", err)
	}
}
