package importxui

import (
	"encoding/json"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/gorm"
)

func (s *importState) recordAudit(tx *gorm.DB, opts Options) error {
	now := time.Now().Unix()
	if opts.Now != nil {
		now = opts.Now()
	}
	details, err := auditDetails(s.report.Summary)
	if err != nil {
		return err
	}
	return tx.Create(&model.AuditEvent{
		DateTime: now,
		Actor:    "system",
		Event:    "xui_import",
		Resource: "database",
		Severity: "info",
		Details:  json.RawMessage(details),
	}).Error
}
