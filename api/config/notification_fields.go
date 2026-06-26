package config

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/util/redact"
	"github.com/gin-gonic/gin"
)

func (a *Handler) coreRestartFailureFields(c *gin.Context, err error) map[string]string {
	sum := sha256.Sum256([]byte(c.Request.UserAgent()))
	return map[string]string{
		"ip":         a.RemoteIP(c),
		"ua_hash":    hex.EncodeToString(sum[:]),
		"ts":         time.Now().UTC().Format(time.RFC3339),
		"errorClass": coreRestartErrorClass(err),
	}
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
