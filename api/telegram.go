package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) TestTelegram(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "telegram", "admin") {
		return
	}
	result := a.TelegramService.TestTelegram()
	severity := service.AuditSeverityInfo
	details := map[string]any{
		"success": result.Success,
	}
	if !result.Success {
		severity = service.AuditSeverityWarn
		details["errorClass"] = result.ErrorClass
	}
	a.recordAudit(c, requestActor(c), "telegram_test", "telegram", severity, details)
	jsonObj(c, result, nil)
}

func (a *ApiService) BackupToTelegram(c *gin.Context) {
	a.runTelegramBackupManual(c)
}

func (a *ApiService) RunTelegramBackup(c *gin.Context) {
	a.runTelegramBackupManual(c)
}

func (a *ApiService) runTelegramBackupManual(c *gin.Context) {
	if !a.requireTokenScopeAny(c, "telegram", "telegram", "admin") {
		return
	}
	if !a.enforceTelegramBackupManualRateLimit(c) {
		return
	}

	backupService := service.TelegramBackupService{
		SettingService:  a.SettingService,
		TelegramService: a.TelegramService,
		AuditService:    a.AuditService,
	}
	ctx := service.ContextWithTelegramBackupActor(c.Request.Context(), requestActor(c))
	result := backupService.RunOnce(ctx, service.TelegramBackupTriggerManual)
	if result.Success {
		c.JSON(http.StatusOK, Msg{
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
	c.JSON(telegramBackupHTTPStatus(errorClass), Msg{
		Success: false,
		Msg:     "telegramBackup: " + errorClass,
		Obj: gin.H{
			"errorClass": errorClass,
			"trigger":    service.TelegramBackupTriggerManual,
		},
	})
}

func (a *ApiService) enforceTelegramBackupManualRateLimit(c *gin.Context) bool {
	actor := requestActor(c)
	key := actor
	if key == "" {
		key = getRemoteIp(c)
	}
	if key == "" {
		key = "unknown"
	}
	retryAfter, err := checkTelegramBackupManualRateLimit(key)
	if err == nil {
		return true
	}
	retrySeconds := int((retryAfter + time.Second - 1) / time.Second)
	if retrySeconds < 1 {
		retrySeconds = 1
	}
	a.recordAudit(c, key, "tg_backup_failed", "database", service.AuditSeverityWarn, map[string]any{
		"trigger":           service.TelegramBackupTriggerManual,
		"payloadSizeBytes":  int64(0),
		"envelopeSizeBytes": int64(0),
		"excludedTables":    []string{},
		"channel":           "telegram",
		"errorClass":        "rate_limited",
	})
	c.Header("Retry-After", strconv.Itoa(retrySeconds))
	c.JSON(http.StatusTooManyRequests, Msg{
		Success: false,
		Msg:     "telegramBackup: rate_limited",
		Obj: gin.H{
			"errorClass": "rate_limited",
			"trigger":    service.TelegramBackupTriggerManual,
		},
	})
	return false
}

func telegramBackupHTTPStatus(errorClass string) int {
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
