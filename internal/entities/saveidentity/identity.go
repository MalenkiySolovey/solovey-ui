// Package saveidentity forwards primary-key admission to the Panel owner.
package saveidentity

import (
	owner "github.com/MalenkiySolovey/solovey-ui/backend/storage/saveidentity"
	"gorm.io/gorm"
)

func Validate(tx *gorm.DB, action string, id uint, model any) error {
	return owner.Validate(tx, action, id, model)
}
