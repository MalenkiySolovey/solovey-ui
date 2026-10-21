package entityinbounds

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/saveeligibility"
	"gorm.io/gorm"
)

// SaveImportedCandidate reuses the editor's decode/current-protocol and
// capability policy, outbound regeneration and client-link owner. Recreate is
// the importer's existing explicit replacement choice, never an HTTP default.
// The entire command is a savepoint within the import's candidate transaction.
func SaveImportedCandidate(tx *gorm.DB, candidate model.Inbound, recreate bool, hostname string) (model.Inbound, error) {
	if tx == nil {
		return model.Inbound{}, errors.New("inbound persistence is unavailable")
	}
	var saved model.Inbound
	err := tx.Transaction(func(view *gorm.DB) error {
		full, err := candidate.MarshalFull()
		if err != nil {
			return err
		}
		raw, err := json.Marshal(full)
		if err != nil {
			return err
		}
		prepared, err := DecodeForSave(view, raw)
		if err != nil {
			return err
		}
		action := "new"
		if prepared.Id != 0 {
			action = "edit"
		}
		if err := saveeligibility.Validate(view, action, prepared.Id, &model.Inbound{}, "inbounds", prepared.Type); err != nil {
			return err
		}
		if recreate {
			if prepared.Id == 0 {
				return errors.New("inbound replacement target is required")
			}
			var previous model.Inbound
			if err := view.First(&previous, prepared.Id).Error; err != nil {
				return err
			}
			if previous.Tag != prepared.Tag {
				return errors.New("inbound replacement cannot change its target tag")
			}
			// A tag reference remains valid through an atomic same-tag replace.
			// VPN user membership is rebound by its own owner below.
			if err := saveeligibility.Check("inbounds", prepared.Type, "new", ""); err != nil {
				return err
			}
			if err := view.Delete(&previous).Error; err != nil {
				return err
			}
			prepared.Id = 0
			if err := FillAndSave(view, &prepared, hostname); err != nil {
				return err
			}
			if err := view.Model(&model.Inbound{}).Where("id = ?", prepared.Id).Update("sort_order", previous.SortOrder).Error; err != nil {
				return err
			}
			prepared.SortOrder = previous.SortOrder
			if err := entityclients.RebindImportedInbound(view, previous.Id, prepared.Id, hostname); err != nil {
				return err
			}
		} else {
			if err := FillAndSave(view, &prepared, hostname); err != nil {
				return err
			}
		}
		// Use the same credential/link cascade as an editor change. Imported
		// candidates do not directly publish to the running core before commit.
		if strings.TrimSpace(hostname) != "" {
			if err := entityclients.UpdateLinksByInboundChange(view, &[]model.Inbound{prepared}, hostname, ""); err != nil {
				return err
			}
		}
		if _, err := CoreChangeForSavedRow(view, prepared.Id, prepared.Tag, ""); err != nil {
			return err
		}
		saved = prepared
		return nil
	})
	return saved, err
}
