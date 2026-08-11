//go:build !minimal

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"github.com/gin-gonic/gin"
)

func (h Handler) actor(c *gin.Context) string {
	if h.deps.Actor == nil {
		return "unknown"
	}
	return h.deps.Actor(c)
}

func (h Handler) audit(c *gin.Context, event string, details map[string]any) {
	if h.deps.Audit != nil {
		h.deps.Audit(c, h.actor(c), event, "server-protection", "info", details)
	}
}

func parsePage(c *gin.Context, fallback, max int) protectionrepository.PageQuery {
	page := queryInt(c, "page")
	if page < 1 {
		page = 1
	}
	limit := queryInt(c, "limit")
	if limit < 1 {
		limit = fallback
	}
	if limit > max {
		limit = max
	}
	return protectionrepository.PageQuery{Page: page, Limit: limit}
}

func parseID(c *gin.Context, name string) (uint, bool) {
	value, err := strconv.ParseUint(c.Param(name), 10, 32)
	if err != nil || value == 0 {
		writeError(c, http.StatusBadRequest, "validation_error", errors.New("invalid id"))
		return 0, false
	}
	return uint(value), true
}

func queryInt(c *gin.Context, name string) int {
	value, _ := strconv.Atoi(strings.TrimSpace(c.Query(name)))
	return value
}

func queryInt64(c *gin.Context, name string) int64 {
	value, _ := strconv.ParseInt(strings.TrimSpace(c.Query(name)), 10, 64)
	return value
}

func queryBool(c *gin.Context, name string) bool {
	value, _ := strconv.ParseBool(strings.TrimSpace(c.Query(name)))
	return value
}

func decodeJSON(data []byte, value any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, value)
}

func writeError(c *gin.Context, status int, code string, err error) {
	message := code
	if err != nil {
		message += ": " + err.Error()
	}
	c.JSON(status, gin.H{"success": false, "msg": code, "obj": gin.H{"code": code, "message": message}})
}
