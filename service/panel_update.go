package service

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	serviceupdate "github.com/MalenkiySolovey/solovey-ui/service/update"
	"github.com/MalenkiySolovey/solovey-ui/util/redact"
)

func NewPanelUpdateManager() *serviceupdate.Manager {
	return serviceupdate.NewManager(serviceupdate.ManagerOptions{TerminalAudit: writePanelUpdateAudit})
}

func writePanelUpdateAudit(job serviceupdate.UpdateJob, result, errorMessage string) {
	if job.ID == "" {
		return
	}
	details := map[string]any{
		"channel": job.Channel, "from": job.FromVersion, "to": job.ToVersion, "result": result,
	}
	severity := AuditSeverityInfo
	if errorMessage != "" {
		details["error"] = redact.String(errorMessage)
		severity = AuditSeverityWarn
	}
	record, err := buildAuditRecord(AuditEvent{
		Actor: job.Initiator, Event: "panel_update_apply", Resource: "update", Severity: severity, Details: details,
	})
	if err != nil {
		logger.Warning("panel update audit build failed: ", err)
		return
	}
	if err := writeAuditEvents([]model.AuditEvent{record}); err != nil {
		logger.Warning("panel update audit write failed: ", err)
	}
}
