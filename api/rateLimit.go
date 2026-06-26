package api

import (
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/ratelimit"
)

const (
	loginRateLimitWindow  = 15 * time.Minute
	loginRateLimitBlock   = 15 * time.Minute
	loginRateLimitMax     = 5
	loginRateLimitMaxKeys = 4096
	loginRateLimitGCEvery = time.Minute

	loginRateLimitTarpitStep = time.Second
	loginRateLimitTarpitMax  = 8 * time.Second

	auditEndpointRateLimitWindow  = time.Minute
	auditEndpointRateLimitMax     = 60
	auditEndpointRateLimitMaxKeys = 4096
	auditEndpointRateLimitGCEvery = time.Minute

	telegramBackupManualRateLimitWindow  = time.Minute
	telegramBackupManualRateLimitMax     = 3
	telegramBackupManualRateLimitMaxKeys = 4096
	telegramBackupManualRateLimitGCEvery = time.Minute
	updateCheckRateLimitWindow           = 5 * time.Second
)

var (
	loginRateLimiter = ratelimit.NewFailureWindow[string](
		loginRateLimitWindow,
		loginRateLimitMax,
		loginRateLimitBlock,
		loginRateLimitMaxKeys,
		loginRateLimitGCEvery,
		loginRateLimitTarpitStep,
		loginRateLimitTarpitMax,
	)
	auditEndpointRateLimiter = ratelimit.NewFixedWindow[string](
		auditEndpointRateLimitWindow,
		auditEndpointRateLimitMax,
		auditEndpointRateLimitMaxKeys,
		auditEndpointRateLimitGCEvery,
	)
	telegramBackupManualRateLimiter = ratelimit.NewSlidingWindow[string](
		telegramBackupManualRateLimitWindow,
		telegramBackupManualRateLimitMax,
		telegramBackupManualRateLimitMaxKeys,
		telegramBackupManualRateLimitGCEvery,
	)
	updateCheckRateLimiter = ratelimit.NewFixedWindow[string](updateCheckRateLimitWindow, 1, 1, time.Minute)
)

func allowForcedUpdateCheck() bool {
	return updateCheckRateLimiter.Allow("global").Allowed
}

func checkLoginRateLimit(key string) error {
	if !loginRateLimiter.Blocked(key).Allowed {
		return common.NewError("too many login attempts")
	}
	return nil
}

func recordLoginFailure(key string) {
	loginRateLimiter.RecordFailure(key)
}

func resetLoginFailures(key string) {
	loginRateLimiter.Reset(key)
}

func loginRateLimitUserKey(username string) string {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		username = "unknown"
	}
	return "user|" + username
}

func loginUsernameTarpitDelay(key string) time.Duration {
	return loginRateLimiter.TarpitDelay(key)
}

func auditEndpointRateLimitKey(actor string, ip string) string {
	if actor == "" {
		actor = "unknown"
	}
	if ip == "" {
		ip = "unknown"
	}
	return actor + "|" + ip
}

func checkAuditEndpointRateLimit(key string) error {
	if !auditEndpointRateLimiter.Allow(key).Allowed {
		return common.NewError("too many audit requests")
	}
	return nil
}

func checkTelegramBackupManualRateLimit(key string) (time.Duration, error) {
	decision := telegramBackupManualRateLimiter.Allow(key)
	if decision.Allowed {
		return 0, nil
	}
	retryAfter := decision.RetryAfter
	if retryAfter < time.Second {
		retryAfter = time.Second
	}
	return retryAfter, common.NewError("too many telegram backup requests")
}
