package entityinbounds

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/transportcredentials"
	"gorm.io/gorm"
)

// TransportDeliveryCopy borrows from the existing owner after subscription
// admission. Neither the canonical row nor its generated template is mutated.
func TransportDeliveryCopy(db *gorm.DB, row *model.Inbound) (*model.Inbound, error) {
	if row == nil || !transportcredentials.IsMKCP(row.Options) {
		return row, nil
	}
	copy := *row
	var err error
	copy.OutJson, err = transportcredentials.Delivery(db, row.Id, row.Options, row.OutJson)
	if err != nil {
		return nil, err
	}
	return &copy, nil
}

func prepareTransportSeed(tx *gorm.DB, row *model.Inbound) error {
	if !transportcredentials.IsMKCP(row.Options) {
		return nil
	}
	var previous json.RawMessage
	if row.Id != 0 {
		var stored model.Inbound
		if err := tx.Select("id", "type", "options").First(&stored, row.Id).Error; err != nil {
			return err
		}
		if stored.Type == row.Type {
			previous = stored.Options
		}
	}
	value, id, err := transportcredentials.Prepare(tx, "inbounds", row.Id, row.Options, previous, func() (uint, error) {
		placeholder := *row
		placeholder.Options, placeholder.OutJson, placeholder.Tls = json.RawMessage(`{}`), json.RawMessage(`null`), nil
		var err error
		placeholder.SortOrder, err = entityorder.ForSave(tx, &model.Inbound{}, 0)
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
