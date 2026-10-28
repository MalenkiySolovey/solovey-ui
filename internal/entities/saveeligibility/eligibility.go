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

// Historical ownership grants read/export/delete authority, not runtime support
// or admission to the ordinary editor. Availability comes from the active host.
func Check(category, panelType, _, _ string) error {
	fact := entitycapabilities.Resolve(category, panelType)
	if fact.Available {
		return nil
	}
	return &Rejection{Capability: fact}
}

func Validate(tx *gorm.DB, action string, id uint, model any, category, panelType string) error {
	if err := saveidentity.Validate(tx, action, id, model); err != nil {
		return err
	}
	if action == "edit" {
		var stored struct {
			Type    string
			Options []byte
		}
		if err := tx.Model(model).Select("type", "options").Where("id = ?", id).Scan(&stored).Error; err != nil {
			return err
		}
		if fact := entitycapabilities.ResolveResource(category, stored.Type, stored.Options); !fact.Available {
			return &Rejection{Capability: fact}
		}
	}
	return Check(category, panelType, action, "")
}
