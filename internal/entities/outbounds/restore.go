package outbounds

import (
	"encoding/json"

	settingcatalog "github.com/MalenkiySolovey/solovey-backend/settings/catalog"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"gorm.io/gorm"
)

// ProjectZapretForValidation omits the attachment only in a private dry
// candidate. The caller must check the binding with CheckZapretStartup and
// discard the rendered config. This function never authorizes a dialer.
func ProjectZapretForValidation(db *gorm.DB) ([]json.RawMessage, error) {
	return getAllConfig(db, true)
}

// ProjectHeldRestoreForValidation renders a transient offline view. The archive
// must already contain a deliberate maintenance hold. This cannot change the
// attachment, authorize dialing, or be used for normal runtime assembly.
func ProjectHeldRestoreForValidation(db *gorm.DB) ([]json.RawMessage, error) {
	var values []string
	if err := db.Model(&model.Setting{}).Where("key = ?", settingcatalog.CoreMaintenanceKey).Limit(2).Pluck("value", &values).Error; err != nil {
		return nil, err
	}
	if len(values) != 1 || values[0] != "true" {
		return nil, ErrZapretPacketPathUnqualified
	}
	return getAllConfig(db, true)
}
