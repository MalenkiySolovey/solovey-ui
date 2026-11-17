// Package auditrecord forwards the existing bounded audit record operations.
package auditrecord

import (
	backendaudit "github.com/MalenkiySolovey/solovey-ui/backend/audit/record"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"gorm.io/gorm"
)

const (
	SeverityInfo      = backendaudit.SeverityInfo
	SeverityWarn      = backendaudit.SeverityWarn
	ActorMaxBytes     = backendaudit.ActorMaxBytes
	EventMaxBytes     = backendaudit.EventMaxBytes
	ResourceMaxBytes  = backendaudit.ResourceMaxBytes
	SeverityMaxBytes  = backendaudit.SeverityMaxBytes
	IPMaxBytes        = backendaudit.IPMaxBytes
	UserAgentMaxBytes = backendaudit.UserAgentMaxBytes
	DetailsMaxBytes   = backendaudit.DetailsMaxBytes
)

type Event = backendaudit.Event

func Build(event Event) (model.AuditEvent, error) { return backendaudit.Build(event) }
func RecordTransaction(tx *gorm.DB, event Event, at int64) error {
	return backendaudit.RecordTransaction(tx, event, at)
}
