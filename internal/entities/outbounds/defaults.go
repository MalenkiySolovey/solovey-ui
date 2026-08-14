package outbounds

import (
	"encoding/json"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityidentity "github.com/MalenkiySolovey/solovey-ui/internal/entities/identity"
	"gorm.io/gorm"
)

// EnsureDefault owns the initial direct fallback fact. It preserves any
// operator-defined non-empty outbound set and fills an existing empty schema
// as well as a fresh installation.
func EnsureDefault(db *gorm.DB) error {
	if db == nil {
		return errors.New("outbound persistence is unavailable")
	}
	var count int64
	if err := db.Model(&model.Outbound{}).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	if err := entityidentity.EnsureOutboundTagAvailable(db, DirectTag, 0, 0); err != nil {
		return err
	}
	return db.Create(&model.Outbound{
		SortOrder: 1,
		Type:      "direct",
		Tag:       DirectTag,
		Options:   json.RawMessage(`{}`),
	}).Error
}
