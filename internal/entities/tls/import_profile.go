package entitytls

import (
	"encoding/json"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

// SaveImportedProfile retains explicit replacement (a new profile ID), the
// editor's in-use deletion denial and its profile/provider/pin/JSON validation.
// It returns the canonical row identity without exposing private persistence.
func SaveImportedProfile(tx *gorm.DB, candidate model.Tls, replaceID uint) (model.Tls, error) {
	if tx == nil {
		return model.Tls{}, errors.New("TLS persistence is unavailable")
	}
	var saved model.Tls
	err := tx.Transaction(func(view *gorm.DB) error {
		var previous model.Tls
		if replaceID != 0 {
			if err := view.First(&previous, replaceID).Error; err != nil {
				return err
			}
			rawID, err := json.Marshal(replaceID)
			if err != nil {
				return err
			}
			if err := Delete(view, rawID); err != nil {
				return err
			}
		}
		candidate.Id = 0
		raw, err := json.Marshal(candidate)
		if err != nil {
			return err
		}
		saved, err = saveConfig(view, "new", raw)
		if err != nil {
			return err
		}
		if replaceID != 0 {
			if err := view.Model(&model.Tls{}).Where("id = ?", saved.Id).Update("sort_order", previous.SortOrder).Error; err != nil {
				return err
			}
			saved.SortOrder = previous.SortOrder
		}
		return nil
	})
	return saved, err
}
