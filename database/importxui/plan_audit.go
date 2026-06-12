package importxui

import (
	"encoding/json"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/gorm"
)

func recordAuditWithBackup(tx *gorm.DB, report *Report, opts ApplyOptions) error {
	now := time.Now().Unix()
	if opts.Now != nil {
		now = opts.Now()
	}
	details := summaryDetails(report.Summary)
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	return tx.Create(&model.AuditEvent{
		DateTime: now,
		Actor:    "system",
		Event:    "xui_import",
		Resource: "database",
		Severity: "info",
		Details:  raw,
	}).Error
}
