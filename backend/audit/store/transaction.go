package store

import (
	auditrecord "github.com/MalenkiySolovey/solovey-ui/backend/audit/record"
	"gorm.io/gorm"
)

// RecordTransaction keeps an owner's committed outcome and its audit record in
// the same transaction, retaining the common audit redaction and size limits.
func RecordTransaction(tx *gorm.DB, event Event, at int64) error {
	return auditrecord.RecordTransaction(tx, event, at)
}
