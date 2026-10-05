// Package saveeligibility adapts build facts to the entity mutation contract.
// It never grants authorization, writes rows or applies runtime changes.
package saveeligibility

import (
	"fmt"

	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/saveidentity"
	"gorm.io/gorm"
)

type Rejection struct{ Capability entitycapabilities.Fact }

func (e *Rejection) Error() string {
	return fmt.Sprintf("%s: %s type %q is unavailable", e.ReasonCode(), e.Capability.Category, e.Capability.Type)
}
func (e *Rejection) ReasonCode() string { return e.Capability.Reason }

// Check permits only a known, correctly contextualized same-type historical
// edit when its implementation is unavailable. The caller supplies storedType
// from an authoritative row, never from a submitted form.
func Check(category, panelType, action, storedType string) error {
	fact := entitycapabilities.Resolve(category, panelType)
	if fact.Available || (action == "edit" && fact.Known && fact.ContextSupported && storedType == panelType) {
		return nil
	}
	return &Rejection{Capability: fact}
}

func Validate(tx *gorm.DB, action string, id uint, model any, category, panelType string) error {
	if err := saveidentity.Validate(tx, action, id, model); err != nil {
		return err
	}
	var storedType string
	if action == "edit" {
		if err := tx.Model(model).Select("type").Where("id = ?", id).Scan(&storedType).Error; err != nil {
			return err
		}
	}
	return Check(category, panelType, action, storedType)
}
