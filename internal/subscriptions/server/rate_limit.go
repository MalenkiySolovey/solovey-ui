package server

import (
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/ratelimit"
	"github.com/gin-gonic/gin"
)

const (
	RateLimitWindow          = time.Minute
	DefaultRateLimitRequests = 60
	RateLimitSettingTTL      = time.Minute
	RateLimitMaxKeys         = 4096
	RateLimitGCEvery         = time.Minute
)

var (
	subscriptionRateLimiter = ratelimit.NewFixedWindow[string](RateLimitWindow, DefaultRateLimitRequests, RateLimitMaxKeys, RateLimitGCEvery)

	rateLimitSettingMu sync.Mutex
	rateLimitSetting   = struct {
		limit     int
		expiresAt time.Time
	}{}
)

func RateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := CanonicalClientIP(c.ClientIP())
		if ip == "" {
			ip = c.ClientIP()
		}
		now := time.Now()
		limit := currentRateLimitRequests(now)
		decision := subscriptionRateLimiter.AllowWithLimitAt(ip, limit, now)
		if !decision.Allowed {
			retryAfter := int(math.Ceil(decision.RetryAfter.Seconds()))
			if retryAfter <= 0 {
				retryAfter = int(RateLimitWindow / time.Second)
			}
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.AbortWithStatus(http.StatusTooManyRequests)
			return
		}
		c.Next()
	}
}

func CanonicalClientIP(value string) string {
	value = strings.TrimSpace(strings.Trim(value, "[]"))
	if value == "" || strings.Contains(value, "%") {
		return ""
	}
	addr, err := netip.ParseAddr(value)
	if err != nil || addr.Zone() != "" {
		return ""
	}
	return addr.Unmap().String()
}

func ResetRateLimitForTest() {
	subscriptionRateLimiter.ResetAll()
	rateLimitSettingMu.Lock()
	rateLimitSetting.limit = 0
	rateLimitSetting.expiresAt = time.Time{}
	rateLimitSettingMu.Unlock()
}

func currentRateLimitRequests(now time.Time) int {
	rateLimitSettingMu.Lock()
	defer rateLimitSettingMu.Unlock()
	if rateLimitSetting.limit > 0 && now.Before(rateLimitSetting.expiresAt) {
		return rateLimitSetting.limit
	}
	limit := DefaultRateLimitRequests
	if provider := SubRateLimitProvider; provider != nil {
		if configured, err := provider(); err == nil && configured > 0 {
			limit = configured
		}
	}
	rateLimitSetting.limit = limit
	rateLimitSetting.expiresAt = now.Add(RateLimitSettingTTL)
	return limit
}
