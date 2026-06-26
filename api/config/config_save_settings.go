package config

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *Handler) handleSettingsSaveError(c *gin.Context, actor string, obj string, err error) bool {
	if obj != "settings" {
		return false
	}
	event := "settings_save_rejected"
	invalidSettingKey := strings.Contains(err.Error(), "invalid setting key:")
	if invalidSettingKey {
		event = "settings_save_rejected_key"
	}
	a.Audit(c, actor, event, "settings", service.AuditSeverityWarn, map[string]any{
		"reason": err.Error(),
	})
	if invalidSettingKey {
		c.JSON(http.StatusBadRequest, Envelope{Success: false, Msg: "save: " + err.Error()})
		return true
	}
	return false
}

func (a *Handler) recordSettingsSaveSucceeded(c *gin.Context, actor string, obj string, action string) {
	if obj != "settings" {
		return
	}
	a.Audit(c, actor, "settings_save_succeeded", "settings", service.AuditSeverityInfo, map[string]any{
		"action": action,
	})
}

func (a *Handler) subscriptionPathSnapshot(obj string, data string) map[string]string {
	if obj != "settings" {
		return nil
	}
	var settings map[string]string
	if err := json.Unmarshal([]byte(data), &settings); err != nil {
		return nil
	}

	before := make(map[string]string, 3)
	if _, ok := settings["subPath"]; ok {
		if path, err := a.SettingService.GetSubPath(); err == nil {
			before["subPath"] = path
		}
	}
	if _, ok := settings["subJsonPath"]; ok {
		if path, err := a.SettingService.GetSubJsonPath(); err == nil {
			before["subJsonPath"] = path
		}
	}
	if _, ok := settings["subClashPath"]; ok {
		if path, err := a.SettingService.GetSubClashPath(); err == nil {
			before["subClashPath"] = path
		}
	}
	if len(before) == 0 {
		return nil
	}
	return before
}

func (a *Handler) auditSubscriptionPathChanges(c *gin.Context, actor string, before map[string]string) {
	if len(before) == 0 {
		return
	}
	changed := map[string]map[string]string{}
	for key, oldPath := range before {
		var newPath string
		var err error
		switch key {
		case "subPath":
			newPath, err = a.SettingService.GetSubPath()
		case "subJsonPath":
			newPath, err = a.SettingService.GetSubJsonPath()
		case "subClashPath":
			newPath, err = a.SettingService.GetSubClashPath()
		default:
			continue
		}
		if err != nil || newPath == oldPath {
			continue
		}
		changed[key] = map[string]string{
			"old": oldPath,
			"new": newPath,
		}
	}
	if len(changed) == 0 {
		return
	}
	a.Audit(c, actor, "sub_path_changed", "subscription", service.AuditSeverityWarn, map[string]any{
		"paths":           changed,
		"restartRequired": true,
	})
}
