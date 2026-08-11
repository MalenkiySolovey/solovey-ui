package dbtransfer

import (
	"errors"
	"net/http"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/service"
	datalifecycle "github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"

	"github.com/gin-gonic/gin"
)

func (a *Handler) ImportDb(c *gin.Context) {
	if !a.RequireScope(c, "database", "admin") {
		return
	}
	if a.RequireStepUp == nil {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"success": false,
			"msg":     "Browser step-up is required",
			"obj":     nil,
		})
		return
	}
	if a.EnforceStepUpHeader && strings.TrimSpace(c.GetHeader("X-Step-Up-Token")) == "" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"success": false,
			"msg":     "Browser step-up is required",
			"obj":     nil,
		})
		return
	}
	prepared, ok := a.openDatabaseImportFile(c)
	if !ok {
		return
	}
	defer prepared.Close()
	rehearsal, err := backup.Rehearse(c.Request.Context(), prepared.MultipartFile())
	if err != nil || !rehearsal.Possible {
		if err == nil {
			err = errors.New("restore rehearsal rejected: " + strings.Join(rehearsal.ReasonCodes, ","))
		}
		a.respondDatabaseImportResult(c, err)
		return
	}
	expected := strings.TrimSpace(c.PostForm("expectedRehearsalRevision"))
	idempotencyKey := strings.TrimSpace(c.PostForm("idempotencyKey"))
	confirmation := strings.TrimSpace(c.PostForm("confirmation"))
	if expected != rehearsal.Revision || confirmation != datalifecycle.RestoreConfirmation(expected) ||
		c.PostForm("acknowledged") != "true" || !safeRestoreControlID(idempotencyKey) {
		a.respondDatabaseImportResult(c, errors.New("restore execution controls are invalid"))
		return
	}
	if !a.RequireStepUp(c, "backup.restore", "database:restore:"+rehearsal.Revision) {
		return
	}
	if _, err := prepared.MultipartFile().Seek(0, 0); err != nil {
		a.respondDatabaseImportResult(c, err)
		return
	}
	manager := a.DataLifecycle
	if manager == nil {
		manager = datalifecycle.Shared()
	}
	operation, result, err := manager.ExecuteRestore(c.Request.Context(), datalifecycle.RestoreRequest{
		ExpectedRehearsalRevision: expected, IdempotencyKey: idempotencyKey, Confirmation: confirmation,
		Acknowledged: true, Source: prepared.MultipartFile(),
	})
	if err != nil {
		a.respondDatabaseImportFailure(c, err)
		c.JSON(http.StatusOK, gin.H{"success": false, "msg": datalifecycle.ReasonCode(err), "obj": gin.H{
			"operation": operation, "execution": result, "rehearsal": rehearsal, "reasonCode": datalifecycle.ReasonCode(err),
		}})
		return
	}
	a.Audit(c, a.Actor(c), "db_imported", "database", service.AuditSeverityWarn, map[string]any{
		"operationId": operation.OperationID, "state": operation.State, "revision": operation.Revision,
		"backupId": operation.ManifestDigest, "recoveryCleanupPending": result.RecoveryCleanupPending,
		"restartPending": result.RestartPending,
	})
	c.JSON(http.StatusOK, gin.H{"success": true, "msg": "", "obj": gin.H{
		"operation": operation, "execution": result, "rehearsal": result.Rehearsal,
	}})
}

func (a *Handler) RehearseDb(c *gin.Context) {
	if !a.RequireScope(c, "database", "admin") {
		return
	}
	prepared, ok := a.openDatabaseImportFile(c)
	if !ok {
		return
	}
	defer prepared.Close()
	if hasRestoreExecutionFields(c) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "msg": "restore_rehearsal_fields_invalid", "obj": nil})
		return
	}
	rehearsal, err := backup.Rehearse(c.Request.Context(), prepared.MultipartFile())
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "msg": "restore_rehearsal_failed", "obj": rehearsal})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "msg": "", "obj": rehearsal})
}

func hasRestoreExecutionFields(c *gin.Context) bool {
	for _, key := range []string{"expectedRehearsalRevision", "idempotencyKey", "confirmation", "acknowledged"} {
		if c.PostForm(key) != "" {
			return true
		}
	}
	return false
}

func safeRestoreControlID(value string) bool {
	if len(value) < 16 || len(value) > 96 {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._:@+-", char) {
			continue
		}
		return false
	}
	return true
}
