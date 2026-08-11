package auth

import (
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"strings"

	"github.com/gin-gonic/gin"
)

func (a *Handler) GetUsers(c *gin.Context) {
	users, err := a.UserService.GetUsers()
	if err != nil {
		a.JSONMsg(c, "", err)
		return
	}
	loginUser := a.LoginUser(c)
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
	a.JSONObj(c, result, nil)
}

func (a *Handler) ChangePass(c *gin.Context) {
	oldPass := c.Request.FormValue("oldPass")
	newUsername := c.Request.FormValue("newUsername")
	newPass := c.Request.FormValue("newPass")
	if !a.requireStepUp(c, "admin.credential", "$self") {
		return
	}
	// Bind the change to the authenticated session user; never trust a target id
	// from the request, so one admin cannot change another admin's credentials.
	currentUser := a.LoginUser(c)
	err := a.UserService.ChangePass(currentUser, oldPass, newUsername, newPass)
	if err == nil {
		logger.Info("change user credentials success")
		a.Audit(c, currentUser, "admin_credentials_changed", "admin", service.AuditSeverityWarn, map[string]any{
			"newUsername": newUsername,
		})
		a.NotifyEvent("admin_credentials_changed", map[string]string{"user": currentUser})
		// Rotate the session generation so every OTHER web session and all WS
		// tokens (including any minted under the old credentials) are invalidated,
		// then re-establish only THIS session under the new generation so the
		// admin who changed the password is not logged out of their own session.
		if newGen, rerr := a.SettingService.RotateSessionGeneration(); rerr != nil {
			logger.Warning("session rotation after credential change failed:", rerr)
		} else {
			sessionMaxAge, _ := a.SettingService.GetSessionMaxAge()
			if serr := a.SetLoginUser(c, newUsername, sessionMaxAge, newGen); serr != nil {
				logger.Warning("re-establishing session after credential change failed:", serr)
			} else {
				a.NotifyEvent("login_success", map[string]string{
					"user":            newUsername,
					"ip":              a.RemoteIP(c),
					"sessionRevision": sessionRevision(newGen),
				})
			}
		}
		a.JSONMsg(c, "save", nil)
	} else {
		logger.Warning("change user credentials failed:", err)
		a.JSONMsg(c, "", err)
	}
}

func (a *Handler) AddAdmin(c *gin.Context) {
	loginUser := a.LoginUser(c)
	username := c.Request.FormValue("username")
	if !a.requireStepUp(c, "admin.create", "new-admin:"+strings.TrimSpace(username)) {
		return
	}
	user, err := a.UserService.AddUser(
		loginUser,
		c.Request.FormValue("currentPass"),
		username,
		c.Request.FormValue("password"),
	)
	if err == nil {
		logger.Info("admin user created successfully")
		a.Audit(c, loginUser, "admin_created", "admin", service.AuditSeverityWarn, map[string]any{
			"targetUserId": user.Id,
			"username":     user.Username,
		})
		a.JSONMsgObj(c, "add", gin.H{
			"id":        user.Id,
			"username":  user.Username,
			"lastLogin": user.LastLogins,
			"isCurrent": false,
		}, nil)
	} else {
		logger.Warning("create admin user failed:", err)
		a.JSONMsg(c, "", err)
	}
}

func (a *Handler) DeleteAdmin(c *gin.Context) {
	loginUser := a.LoginUser(c)
	targetID := c.Request.FormValue("id")
	if !a.requireStepUp(c, "admin.delete", "user:"+strings.TrimSpace(targetID)) {
		return
	}
	result, err := a.UserService.DeleteUser(
		loginUser,
		c.Request.FormValue("currentPass"),
		targetID,
	)
	if err == nil {
		logger.Info("admin user deleted successfully")
		a.Audit(c, loginUser, "admin_deleted", "admin", service.AuditSeverityWarn, map[string]any{
			"targetUserId":      result.User.Id,
			"username":          result.User.Username,
			"deletedTokenCount": result.DeletedTokenCount,
		})
		a.NotifyEvent("admin_deleted", map[string]string{"user": result.User.Username})
		a.JSONMsg(c, "del", nil)
	} else {
		logger.Warning("delete admin user failed:", err)
		a.JSONMsg(c, "", err)
	}
}
