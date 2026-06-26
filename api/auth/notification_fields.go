package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/gin-gonic/gin"
)

func (a *Handler) telegramRequestFields(c *gin.Context) map[string]string {
	sum := sha256.Sum256([]byte(c.Request.UserAgent()))
	return map[string]string{
		"ip":      a.RemoteIP(c),
		"ua_hash": hex.EncodeToString(sum[:]),
		"ts":      time.Now().UTC().Format(time.RFC3339),
	}
}
