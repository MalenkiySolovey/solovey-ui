package service

import (
	logger "github.com/MalenkiySolovey/solovey-ui/logger"
	coreservice "github.com/MalenkiySolovey/solovey-ui/service"
	serviceupdate "github.com/MalenkiySolovey/solovey-ui/service/update"
	"github.com/MalenkiySolovey/solovey-ui/util/redact"
)

func NewUpdateManager() *serviceupdate.Manager {
	return serviceupdate.NewManager(serviceupdate.ManagerOptions{TerminalAudit: writePanelUpdateAudit})
}

func writePanelUpdateAudit(job serviceupdate.UpdateJob, result, errorMessage string) {
	if job.ID == "" {
		return
	}
	details := map[string]any{
		"channel": job.Channel,
		"from":    job.FromVersion,
		"to":      job.ToVersion,
		"result":  result,
	}
	severity := coreservice.AuditSeverityInfo
	if errorMessage != "" {
		details["error"] = redact.String(errorMessage)
		severity = coreservice.AuditSeverityWarn
	}
	if err := (&coreservice.AuditService{}).Record(coreservice.AuditEvent{
		Actor:    job.Initiator,
		Event:    "panel_update_apply",
		Resource: "update",
		Severity: severity,
		Details:  details,
	}); err != nil {
		logger.Warning("panel update audit write failed: ", err)
	}
}
