//go:build !minimal

package importxui

import (
	auditsvc "github.com/MalenkiySolovey/solovey-ui/service/audit"
	"gorm.io/gorm"
	"time"
)

func recordAuditWithBackup(tx *gorm.DB, report *Report, opts ApplyOptions) error {
	now := time.Now().Unix()
	if opts.Now != nil {
		now = opts.Now()
	}
	details := summaryDetails(report.Summary)
	details["operationId"] = report.Change.OperationID
	details["planRevision"] = report.Change.ExpectedRevision
	details["sha256"] = opts.sourceHash
	if report.BackupPath != "" {
		details["backup"] = report.BackupPath
	}
	actor := opts.Actor
	if actor == "" {
		actor = "system"
	}
	return auditsvc.RecordTransaction(tx, auditsvc.Event{Actor: actor, Event: "panel_import",
		Resource: "database", Severity: auditsvc.AuditSeverityInfo, IP: opts.RemoteIP, Details: details}, now)
}
