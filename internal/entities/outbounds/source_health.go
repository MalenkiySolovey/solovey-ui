package outbounds

import (
	hostoutbounds "github.com/MalenkiySolovey/solovey-host/outbounds"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityidentity "github.com/MalenkiySolovey/solovey-ui/internal/entities/identity"
	"gorm.io/gorm"
)

// MarkSourceMissing changes only source-health metadata on an exact Outbound.
// Resource mutation remains with this owner; caller retains its transaction.
func MarkSourceMissing(tx *gorm.DB, id uint, tag string, missing hostoutbounds.Missing) error {
	result := tx.Model(&model.Outbound{}).Where("id = ? AND tag = ?", id, tag).Updates(map[string]any{
		"remote_missing": true, "remote_missing_reason": missing.Reason, "remote_missing_since": missing.Since, "remote_missing_source": missing.Source,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return hostoutbounds.ErrRevision
	}
	return nil
}

// RetagSource changes only the tag of the exact accepted source-managed row.
// Validation and the before-image check remain with the canonical writer.
func RetagSource(tx *gorm.DB, id uint, oldTag, newTag string) (bool, error) {
	if err := entityidentity.ValidateTag(newTag); err != nil {
		return false, err
	}
	if err := entityidentity.EnsureOutboundTagAvailable(tx, newTag, id, 0); err != nil {
		return false, err
	}
	result := tx.Model(&model.Outbound{}).Where("id = ? AND tag = ?", id, oldTag).Update("tag", newTag)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}
