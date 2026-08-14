// Package entities coordinates domain-owned validation after migration or
// restore. Each subpackage remains the authority for its own invariant.
package entities

import (
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	entityendpoints "github.com/MalenkiySolovey/solovey-ui/internal/entities/endpoints"
	entityidentity "github.com/MalenkiySolovey/solovey-ui/internal/entities/identity"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	entityservices "github.com/MalenkiySolovey/solovey-ui/internal/entities/services"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	"gorm.io/gorm"
)

func ValidateStored(db *gorm.DB) error {
	validators := []func(*gorm.DB) error{
		entityidentity.ValidateStored,
		entityclients.ValidateStored,
		entityinbounds.ValidateStored,
		entityoutbounds.ValidateStored,
		entityendpoints.ValidateStored,
		entityservices.ValidateStored,
		entitytls.ValidateStored,
	}
	for _, validate := range validators {
		if err := validate(db); err != nil {
			return err
		}
	}
	return nil
}
