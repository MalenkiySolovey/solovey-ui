package auth

import (
	"context"
	"strconv"
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
		a.NotifyEvent("login_blocked", a.securityEventRequestFields(c))
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
	authResult, err := a.UserService.Authenticate(context.Background(), username, c.Request.FormValue("pass"), remoteIP)
	if err != nil {
		a.RecordLoginFailure(remoteIP)
		a.RecordLoginFailure(userKey)
		a.Audit(c, username, "login_failed", "auth", service.AuditSeverityWarn, map[string]any{
			"reason": "authentication_failed",
		})
		a.NotifyEvent("login_failed", a.securityEventRequestFields(c))
		a.JSONMsg(c, "", err)
		return
	}
	a.ResetLoginFailures(remoteIP)
	a.ResetLoginFailures(userKey)
	loginUser := authResult.Username()

	sessionLifetime, err := a.SettingService.ResolveSessionLifetime()
	if err != nil {
		logger.Warning("unable to resolve session lifetime policy:", err)
		a.JSONMsg(c, "", err)
		return
	}

	sessionGeneration, err := a.SettingService.GetSessionGeneration()
	if err != nil {
		logger.Warning("unable to get session generation:", err)
	}

	remembered, _ := strconv.ParseBool(c.Request.FormValue("remember"))
	err = a.SetLoginSecurity(c, service.LoginSessionSpec{
		UserID:               authResult.UserID(),
		Username:             loginUser,
		AuthState:            authResult.AuthState,
		Assurance:            authResult.Assurance,
		LifetimePosture:      sessionLifetime.Posture,
		SessionGeneration:    sessionGeneration,
		CredentialGeneration: authResult.CredentialGeneration(),
		MFAGeneration:        authResult.MFAGeneration(),
		Remembered:           remembered,
		ClientProvenance:     "resolved",
		ClientPrefix:         remoteIP,
		UserAgentHash:        service.UserAgentDigest(c.Request.UserAgent()),
		DeviceLabel:          boundedUserAgent(c.Request.UserAgent()),
		LegacyMaxAge:         sessionLifetime.LegacyMaxAge,
	})
	if err != nil {
		logger.Warning("login failed: ", err)
		a.Audit(c, loginUser, "login_session_failed", "auth", service.AuditSeverityWarn, map[string]any{
			"reason": "session_establishment_failed",
		})
		a.JSONMsg(c, "", err)
		return
	}
	logger.Info("admin login success")
	a.Audit(c, loginUser, "login_success", "auth", service.AuditSeverityInfo, nil)
	a.NotifyEvent("login_success", map[string]string{
		"user":            loginUser,
		"ip":              remoteIP,
		"sessionRevision": sessionRevision(sessionGeneration),
	})

	a.JSONMsgObj(c, "", gin.H{
		"state":     authResult.AuthState,
		"assurance": authResult.Assurance,
	}, nil)
}

func (a *Handler) Logout(c *gin.Context) {
	loginUser := a.LoginUser(c)
	if loginUser != "" {
		logger.Info("admin logout")
		a.Audit(c, loginUser, "logout", "auth", service.AuditSeverityInfo, nil)
		a.NotifyEvent("logout", map[string]string{"user": loginUser})
	}
	a.ClearSession(c)
	a.JSONMsg(c, "", nil)
}

func sessionRevision(generation string) string {
	return service.SessionGenerationRevision(generation)
}

func boundedUserAgent(value string) string {
	if len(value) <= 96 {
		return value
	}
	return value[:96]
}

func (a *Handler) LogoutAllAdmins(c *gin.Context) {
	loginUser := a.LoginUser(c)
	_, err := a.SettingService.RotateSessionGeneration()
	if err == nil {
		if loginUser != "" {
			logger.Info("all admin web sessions logged out")
		}
		a.Audit(c, loginUser, "logout_all_admins", "auth", service.AuditSeverityWarn, nil)
		a.NotifyEvent("logout_all_admins", map[string]string{
			"user": loginUser,
		})
		a.ClearSession(c)
	}
	a.JSONMsg(c, "logoutAllAdmins", err)
}
