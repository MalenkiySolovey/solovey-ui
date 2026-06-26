// Package telegram owns Telegram operations exposed through the HTTP API.
package telegram

import (
	"net/http"
	"strconv"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	Settings       service.SettingService
	Telegram       service.TelegramService
	AuditService   service.AuditService
	RequireScope   func(*gin.Context, string, ...string) bool
	Actor          func(*gin.Context) string
	RemoteIP       func(*gin.Context) string
	CheckRateLimit func(string) (time.Duration, error)
	Audit          func(*gin.Context, string, string, string, string, map[string]any)
	JSONObj        func(*gin.Context, interface{}, error)
}

// Deps contains the host capabilities required by Telegram API routes.
type Deps struct {
	Settings       service.SettingService
	Telegram       service.TelegramService
	AuditService   service.AuditService
	RequireScope   func(*gin.Context, string, ...string) bool
	Actor          func(*gin.Context) string
	RemoteIP       func(*gin.Context) string
	CheckRateLimit func(string) (time.Duration, error)
	Audit          func(*gin.Context, string, string, string, string, map[string]any)
	JSONObj        func(*gin.Context, interface{}, error)
}

type Envelope struct {
	Success bool        `json:"success"`
	Msg     string      `json:"msg"`
	Obj     interface{} `json:"obj"`
}

// RegisterRoutes mounts Telegram endpoints on an already secured API group.
func RegisterRoutes(g *gin.RouterGroup, deps Deps) {
	h := &Handler{
		Settings:       deps.Settings,
		Telegram:       deps.Telegram,
		AuditService:   deps.AuditService,
		RequireScope:   deps.RequireScope,
		Actor:          deps.Actor,
		RemoteIP:       deps.RemoteIP,
		CheckRateLimit: deps.CheckRateLimit,
		Audit:          deps.Audit,
		JSONObj:        deps.JSONObj,
	}
	group := g.Group("/telegram")
	group.POST("/test", h.TestTelegram)
	group.POST("/backup", h.BackupToTelegram)
	group.POST("/backup/run", h.RunTelegramBackup)
}

func (a *Handler) TestTelegram(c *gin.Context) {
	if !a.RequireScope(c, "telegram", "admin") {
		return
	}
	result := a.Telegram.TestTelegram()
	severity := service.AuditSeverityInfo
	details := map[string]any{
		"success": result.Success,
	}
	if !result.Success {
		severity = service.AuditSeverityWarn
		details["errorClass"] = result.ErrorClass
	}
	a.Audit(c, a.Actor(c), "telegram_test", "telegram", severity, details)
	a.JSONObj(c, result, nil)
}

func (a *Handler) BackupToTelegram(c *gin.Context) {
	a.runTelegramBackupManual(c)
}

func (a *Handler) RunTelegramBackup(c *gin.Context) {
	a.runTelegramBackupManual(c)
}

func (a *Handler) runTelegramBackupManual(c *gin.Context) {
	if !a.RequireScope(c, "telegram", "telegram", "admin") {
		return
	}
	if !a.enforceTelegramBackupManualRateLimit(c) {
		return
	}

	backupService := service.TelegramBackupService{
		SettingService:  a.Settings,
		TelegramService: a.Telegram,
		AuditService:    a.AuditService,
	}
	ctx := service.ContextWithTelegramBackupActor(c.Request.Context(), a.Actor(c))
	result := backupService.RunOnce(ctx, service.TelegramBackupTriggerManual)
	if result.Success {
		c.JSON(http.StatusOK, Envelope{
			Success: true,
			Obj: gin.H{
				"filename": result.Filename,
				"trigger":  result.Trigger,
			},
		})
		return
	}
	errorClass := result.ErrorClass
	if errorClass == "" {
		errorClass = "internal"
	}
	c.JSON(BackupHTTPStatus(errorClass), Envelope{
		Success: false,
		Msg:     "telegramBackup: " + errorClass,
		Obj: gin.H{
			"errorClass": errorClass,
			"trigger":    service.TelegramBackupTriggerManual,
		},
	})
}

func (a *Handler) enforceTelegramBackupManualRateLimit(c *gin.Context) bool {
	actor := a.Actor(c)
	key := actor
	if key == "" {
		key = a.RemoteIP(c)
	}
	if key == "" {
		key = "unknown"
	}
	retryAfter, err := a.CheckRateLimit(key)
	if err == nil {
		return true
	}
	retrySeconds := int((retryAfter + time.Second - 1) / time.Second)
	if retrySeconds < 1 {
		retrySeconds = 1
	}
	a.Audit(c, key, "tg_backup_failed", "database", service.AuditSeverityWarn, map[string]any{
		"trigger":           service.TelegramBackupTriggerManual,
		"payloadSizeBytes":  int64(0),
		"envelopeSizeBytes": int64(0),
		"excludedTables":    []string{},
		"channel":           "telegram",
		"errorClass":        "rate_limited",
	})
	c.Header("Retry-After", strconv.Itoa(retrySeconds))
	c.JSON(http.StatusTooManyRequests, Envelope{
		Success: false,
		Msg:     "telegramBackup: rate_limited",
		Obj: gin.H{
			"errorClass": "rate_limited",
			"trigger":    service.TelegramBackupTriggerManual,
		},
	})
	return false
}

func BackupHTTPStatus(errorClass string) int {
	switch errorClass {
	case "concurrent_run":
		return http.StatusConflict
	case "rate_limited":
		return http.StatusTooManyRequests
	case "disabled", "missing_token", "missing_chat", "missing_passphrase", "oversize", "network", "proxy", "unauthorized", "chat_not_found":
		return http.StatusServiceUnavailable
	case "db_snapshot_failed", "encryption_failed", "settings", "payload", "request", "unknown", "internal":
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
