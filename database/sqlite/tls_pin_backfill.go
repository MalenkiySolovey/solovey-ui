package sqlite

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"

	"gorm.io/gorm"
)

func backfillTLSClientPins(db *gorm.DB) error {
	var rows []model.Tls
	if err := db.Model(model.Tls{}).Where("id > 0").Find(&rows).Error; err != nil {
		return err
	}
	for i := range rows {
		row := rows[i]
		if !entitytls.ApplySelfSignedPublicKeyPin(&row) {
			continue
		}
		if err := db.Model(model.Tls{}).Where("id = ?", row.Id).Update("client", row.Client).Error; err != nil {
			return err
		}
	}
	return nil
}
