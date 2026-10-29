package endpoints

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/saveeligibility"
	"gorm.io/gorm"
)

// SaveImportedCandidate persists already captured endpoint credentials through
// the same capability, namespace, identity and reference policy as the editor.
// Captured WARP credentials must not cause an implicit Internet registration.
func SaveImportedCandidate(tx *gorm.DB, candidate model.Endpoint, recreate bool) (model.Endpoint, error) {
	if tx == nil {
		return model.Endpoint{}, errors.New("endpoint persistence is unavailable")
	}
	if err := jsonvalue.OptionalObject("endpoint options", candidate.Options); err != nil {
		return model.Endpoint{}, err
	}
	if err := jsonvalue.OptionalObject("endpoint extension", candidate.Ext); err != nil {
		return model.Endpoint{}, err
	}
	if candidate.Type == "warp" {
		var captured struct {
			PrivateKey string `json:"private_key"`
			Peers      []struct {
				PublicKey string `json:"public_key"`
			} `json:"peers"`
		}
		if json.Unmarshal(candidate.Options, &captured) != nil || strings.TrimSpace(captured.PrivateKey) == "" || len(captured.Peers) == 0 {
			return model.Endpoint{}, errors.New("captured WARP endpoint credentials are incomplete")
		}
		for _, peer := range captured.Peers {
			if strings.TrimSpace(peer.PublicKey) == "" {
				return model.Endpoint{}, errors.New("captured WARP peer credentials are incomplete")
			}
		}
	}
	var saved model.Endpoint
	err := tx.Transaction(func(view *gorm.DB) error {
		if err := importEndpointCredentials(view, &candidate, recreate); err != nil {
			return err
		}
		action := "new"
		if candidate.Id != 0 {
			action = "edit"
		}
		var previous model.Endpoint
		if recreate {
			if candidate.Id == 0 {
				return errors.New("endpoint replacement target is required")
			}
			if err := view.First(&previous, candidate.Id).Error; err != nil {
				return err
			}
			if previous.Tag != candidate.Tag {
				return errors.New("endpoint replacement cannot change its target tag")
			}
			if err := saveeligibility.Check("endpoints", candidate.Type, "new", ""); err != nil {
				return err
			}
			if err := view.Delete(&previous).Error; err != nil {
				return err
			}
			candidate.Id = 0
			action = "new"
		}
		if _, err := saveCandidate(view, action, candidate, nil, true); err != nil {
			return err
		}
		if err := view.Where("tag = ?", candidate.Tag).First(&saved).Error; err != nil {
			return err
		}
		if recreate {
			if err := view.Model(&model.Endpoint{}).Where("id = ?", saved.Id).Update("sort_order", previous.SortOrder).Error; err != nil {
				return err
			}
			saved.SortOrder = previous.SortOrder
		}
		return nil
	})
	return saved, err
}
