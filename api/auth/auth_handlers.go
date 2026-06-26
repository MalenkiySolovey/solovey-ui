package auth

import (
	"time"

	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *Handler) Login(c *gin.Context) {
	remoteIP := a.RemoteIP(c)
	username := c.Request.FormValue("user")
	userKey := a.LoginRateLimitUserKey(username)
	// Two independent throttles: per source IP (one attacker host) and per
	// username (a distributed brute-force on one account from rotating IPs,
	// which the per-IP limit alone cannot stop).
	if err := a.CheckLoginRateLimit(remoteIP); err != nil {
		a.Audit(c, username, "login_blocked", "auth", service.AuditSeverityWarn, map[string]any{
			"reason": "rate_limit_ip",
		})
		// Real-time alert on the lockout transition (T1110): brute-force reaching
		// the per-IP block is a high-signal admin-compromise indicator.
		a.TelegramService.NotifyTelegramEvent("login_blocked", a.telegramRequestFields(c))
		a.JSONMsg(c, "", err)
		return
	}
	// Per-username throttle is a tarpit (escalating, capped delay), never a hard
	// block, so a distributed attacker burning failures from rotating IPs cannot
	// lock a known admin out of their own panel. The per-IP hard block above
	// remains the primary brute-force defence.
	if delay := a.LoginUsernameTarpitDelay(userKey); delay > 0 {
		select {
		case <-time.After(delay):
		case <-c.Request.Context().Done():
			return
		}
	}
	loginUser, err := a.UserService.Login(username, c.Request.FormValue("pass"), remoteIP)
	if err != nil {
		a.RecordLoginFailure(remoteIP)
		a.RecordLoginFailure(userKey)
		a.Audit(c, username, "login_failed", "auth", service.AuditSeverityWarn, map[string]any{
			"reason": err.Error(),
		})
		a.TelegramService.NotifyTelegramEvent("login_failed", a.telegramRequestFields(c))
		a.JSONMsg(c, "", err)
		return
	}
	a.ResetLoginFailures(remoteIP)
	a.ResetLoginFailures(userKey)

	sessionMaxAge, err := a.SettingService.GetSessionMaxAge()
	if err != nil {
		logger.Infof("Unable to get session's max age from DB")
	}

	sessionGeneration, err := a.SettingService.GetSessionGeneration()
	if err != nil {
		logger.Warning("unable to get session generation:", err)
	}

	err = a.SetLoginUser(c, loginUser, sessionMaxAge, sessionGeneration)
	if err == nil {
		logger.Info("user ", loginUser, " login success")
		a.Audit(c, loginUser, "login_success", "auth", service.AuditSeverityInfo, nil)
		a.TelegramService.NotifyTelegramEvent("login_success", map[string]string{
			"user": loginUser,
			"ip":   remoteIP,
		})
	} else {
		logger.Warning("login failed: ", err)
		a.Audit(c, loginUser, "login_session_failed", "auth", service.AuditSeverityWarn, map[string]any{
			"reason": err.Error(),
		})
	}

	a.JSONMsg(c, "", nil)
}

func (a *Handler) Logout(c *gin.Context) {
	loginUser := a.LoginUser(c)
	if loginUser != "" {
		logger.Infof("user %s logout", loginUser)
		a.Audit(c, loginUser, "logout", "auth", service.AuditSeverityInfo, nil)
	}
	a.ClearSession(c)
	a.JSONMsg(c, "", nil)
}

func (a *Handler) LogoutAllAdmins(c *gin.Context) {
	loginUser := a.LoginUser(c)
	_, err := a.SettingService.RotateSessionGeneration()
	if err == nil {
		if loginUser != "" {
			logger.Infof("user %s logged out all admin web sessions", loginUser)
		}
		a.Audit(c, loginUser, "logout_all_admins", "auth", service.AuditSeverityWarn, nil)
		a.TelegramService.NotifyTelegramEvent("logout_all_admins", map[string]string{
			"user": loginUser,
		})
		a.ClearSession(c)
	}
	a.JSONMsg(c, "logoutAllAdmins", err)
}
