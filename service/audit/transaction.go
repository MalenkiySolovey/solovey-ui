package audit

import (
	"errors"
	"gorm.io/gorm"
)

// RecordTransaction keeps an owner's committed outcome and its audit record in
// the same transaction, retaining the common audit redaction and size limits.
func RecordTransaction(tx *gorm.DB, event Event, at int64) error {
	if tx == nil || at <= 0 {
		return errors.New("audit transaction is unavailable")
	}
	record, err := BuildRecord(event)
	if err != nil {
		return err
	}
	record.DateTime = at
	return tx.Create(&record).Error
}
