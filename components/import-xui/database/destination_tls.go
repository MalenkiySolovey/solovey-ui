//go:build !minimal

package importxui

import (
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"github.com/MalenkiySolovey/solovey-component-import-xui/mapping"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	"gorm.io/gorm"
)

func findExistingRealityTLS(tx *gorm.DB, spec mapping.RealitySpec) (model.Tls, bool, error) {
	var rows []model.Tls
	if err := tx.Find(&rows).Error; err != nil {
		return model.Tls{}, false, err
	}
	row, found := mapping.FindExistingRealityTLS(rows, spec)
	return row, found, nil
}

func findExistingPlainTLS(tx *gorm.DB, spec mapping.TLSCertSpec) (model.Tls, bool, error) {
	var rows []model.Tls
	if err := tx.Find(&rows).Error; err != nil {
		return model.Tls{}, false, err
	}
	row, found := mapping.FindExistingPlainTLS(rows, spec)
	return row, found, nil
}

func saveImportedTLS(tx *gorm.DB, candidate model.Tls, replaceID uint, hostname string) (model.Tls, error) {
	return entitytls.SaveImportedProfile(tx, candidate, replaceID)
}
