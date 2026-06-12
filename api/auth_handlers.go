package api

import (
	"time"

	"github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) Login(c *gin.Context) {
	remoteIP := getRemoteIp(c)
	username := c.Request.FormValue("user")
	userKey := loginRateLimitUserKey(username)
	// Two independent throttles: per source IP (one attacker host) and per
	// username (a distributed brute-force on one account from rotating IPs,
	// which the per-IP limit alone cannot stop).
	if err := checkLoginRateLimit(remoteIP); err != nil {
		a.recordAudit(c, username, "login_blocked", "auth", service.AuditSeverityWarn, map[string]any{
			"reason": "rate_limit_ip",
		})
		// Real-time alert on the lockout transition (T1110): brute-force reaching
		// the per-IP block is a high-signal admin-compromise indicator.
		a.TelegramService.NotifyTelegramEvent("login_blocked", telegramRequestFields(c))
		jsonMsg(c, "", err)
		return
	}
	// Per-username throttle is a tarpit (escalating, capped delay), never a hard
	// block, so a distributed attacker burning failures from rotating IPs cannot
	// lock a known admin out of their own panel. The per-IP hard block above
	// remains the primary brute-force defence.
	if delay := loginUsernameTarpitDelay(userKey); delay > 0 {
		select {
		case <-time.After(delay):
		case <-c.Request.Context().Done():
			return
		}
	}
	loginUser, err := a.UserService.Login(username, c.Request.FormValue("pass"), remoteIP)
	if err != nil {
		recordLoginFailure(remoteIP)
		recordLoginFailure(userKey)
		a.recordAudit(c, username, "login_failed", "auth", service.AuditSeverityWarn, map[string]any{
			"reason": err.Error(),
		})
		a.TelegramService.NotifyTelegramEvent("login_failed", telegramRequestFields(c))
		jsonMsg(c, "", err)
		return
	}
	resetLoginFailures(remoteIP)
	resetLoginFailures(userKey)

	sessionMaxAge, err := a.SettingService.GetSessionMaxAge()
	if err != nil {
		logger.Infof("Unable to get session's max age from DB")
	}

	sessionGeneration, err := a.SettingService.GetSessionGeneration()
	if err != nil {
		logger.Warning("unable to get session generation:", err)
	}

	err = SetLoginUser(c, loginUser, sessionMaxAge, sessionGeneration)
	if err == nil {
		logger.Info("user ", loginUser, " login success")
		a.recordAudit(c, loginUser, "login_success", "auth", service.AuditSeverityInfo, nil)
		a.TelegramService.NotifyTelegramEvent("login_success", map[string]string{
			"user": loginUser,
			"ip":   remoteIP,
		})
	} else {
		logger.Warning("login failed: ", err)
		a.recordAudit(c, loginUser, "login_session_failed", "auth", service.AuditSeverityWarn, map[string]any{
			"reason": err.Error(),
		})
	}

	jsonMsg(c, "", nil)
}

func (a *ApiService) Logout(c *gin.Context) {
	loginUser := GetLoginUser(c)
	if loginUser != "" {
		logger.Infof("user %s logout", loginUser)
		a.recordAudit(c, loginUser, "logout", "auth", service.AuditSeverityInfo, nil)
	}
	ClearSession(c)
	jsonMsg(c, "", nil)
}

func (a *ApiService) LogoutAllAdmins(c *gin.Context) {
	loginUser := GetLoginUser(c)
	_, err := a.SettingService.RotateSessionGeneration()
	if err == nil {
		if loginUser != "" {
			logger.Infof("user %s logged out all admin web sessions", loginUser)
		}
		a.recordAudit(c, loginUser, "logout_all_admins", "auth", service.AuditSeverityWarn, nil)
		a.TelegramService.NotifyTelegramEvent("logout_all_admins", map[string]string{
			"user": loginUser,
		})
		ClearSession(c)
	}
	jsonMsg(c, "logoutAllAdmins", err)
}
