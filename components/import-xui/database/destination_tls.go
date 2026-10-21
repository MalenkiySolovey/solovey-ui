//go:build !minimal

package importxui

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	"gorm.io/gorm"
)

func saveImportedTLS(tx *gorm.DB, candidate model.Tls, replaceID uint, hostname string) (model.Tls, error) {
	return entitytls.SaveImportedProfile(tx, candidate, replaceID)
}
