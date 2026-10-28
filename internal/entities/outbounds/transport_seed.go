package outbounds

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/transportcredentials"
	"gorm.io/gorm"
)

func prepareTransportSeed(tx *gorm.DB, row *model.Outbound) error {
	var previous json.RawMessage
	if row.Id != 0 {
		var stored model.Outbound
		if err := tx.Select("id", "type", "options").First(&stored, row.Id).Error; err != nil {
			return err
		}
		if stored.Type == row.Type {
			previous = stored.Options
		}
	}
	value, id, err := transportcredentials.Prepare(tx, "outbounds", row.Id, row.Options, previous, func() (uint, error) {
		placeholder := *row
		placeholder.Options = json.RawMessage(`{}`)
		var err error
		placeholder.SortOrder, err = entityorder.ForSave(tx, &model.Outbound{}, 0)
		if err != nil {
			return 0, err
		}
		if err := tx.Create(&placeholder).Error; err != nil {
			return 0, err
		}
		return placeholder.Id, nil
	})
	if err == nil {
		row.Options, row.Id = value, id
	}
	return err
}
