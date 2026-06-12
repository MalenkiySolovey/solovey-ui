package api

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/redact"

	"github.com/gin-gonic/gin"
)

func telegramRequestFields(c *gin.Context) map[string]string {
	return map[string]string{
		"ip":      getRemoteIp(c),
		"ua_hash": hashUserAgent(c.Request.UserAgent()),
		"ts":      time.Now().UTC().Format(time.RFC3339),
	}
}

func hashUserAgent(userAgent string) string {
	sum := sha256.Sum256([]byte(userAgent))
	return hex.EncodeToString(sum[:])
}

func coreRestartFailedTelegramFields(c *gin.Context, err error) map[string]string {
	fields := telegramRequestFields(c)
	fields["errorClass"] = coreRestartErrorClass(err)
	return fields
}

func coreRestartErrorClass(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(redact.String(err.Error()))
	switch {
	case strings.Contains(message, "timeout"), strings.Contains(message, "deadline exceeded"):
		return "timeout"
	case strings.Contains(message, "permission"), strings.Contains(message, "access is denied"):
		return "permission"
	case strings.Contains(message, "config"), strings.Contains(message, "parse"), strings.Contains(message, "json"):
		return "config"
	default:
		return "failed"
	}
}
