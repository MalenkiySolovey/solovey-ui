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

type RateLimiter struct {
	limiter   *ratelimit.FixedWindow[string]
	settingMu sync.Mutex
	setting   struct {
		limit     int
		expiresAt time.Time
	}
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		limiter: ratelimit.NewFixedWindow[string](RateLimitWindow, DefaultRateLimitRequests, RateLimitMaxKeys, RateLimitGCEvery),
	}
}

func (r *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := CanonicalClientIP(c.ClientIP())
		if ip == "" {
			ip = c.ClientIP()
		}
		now := time.Now()
		limit := r.currentRequests(now)
		decision := r.limiter.AllowWithLimitAt(ip, limit, now)
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

func (r *RateLimiter) Close() {
	if r != nil && r.limiter != nil {
		r.limiter.Close()
	}
}

func (r *RateLimiter) currentRequests(now time.Time) int {
	r.settingMu.Lock()
	defer r.settingMu.Unlock()
	if r.setting.limit > 0 && now.Before(r.setting.expiresAt) {
		return r.setting.limit
	}
	limit := DefaultRateLimitRequests
	if provider := currentHooks().RateLimitProvider; provider != nil {
		if configured, err := provider(); err == nil && configured > 0 {
			limit = configured
		}
	}
	r.setting.limit = limit
	r.setting.expiresAt = now.Add(RateLimitSettingTTL)
	return limit
}
