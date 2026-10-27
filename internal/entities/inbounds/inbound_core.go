package entityinbounds

import (
	"encoding/json"
	"os"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	runtimeprojection "github.com/MalenkiySolovey/solovey-ui/internal/entities/runtimeprojection"
	"gorm.io/gorm"
)

func UpdateOutJSONs(tx *gorm.DB, inboundIDs []uint, hostname string) error {
	var inbounds []model.Inbound
	err := tx.Model(model.Inbound{}).Preload("Tls").Where("id in ?", inboundIDs).Find(&inbounds).Error
	if err != nil {
		return err
	}
	for _, inbound := range inbounds {
		err = FillOutboundJSONFromDB(tx, &inbound, hostname)
		if err != nil {
			return err
		}
		err = tx.Model(model.Inbound{}).Where("tag = ?", inbound.Tag).Update("out_json", inbound.OutJson).Error
		if err != nil {
			return err
		}
	}
	return nil
}
func GetAllConfig(db *gorm.DB, hooks UserHooks) ([]json.RawMessage, error) {
	var inboundsJSON []json.RawMessage
	view, err := loadRuntimeInboundView(db)
	if err != nil {
		return nil, err
	}
	for _, inbound := range view.rows {
		if view.groups.Parent(inbound.Tag) != "" || !view.facts[inbound.Tag].Available {
			continue
		}
		inboundJSON, err := view.render(db, inbound, hooks)
		if err != nil {
			return nil, err
		}
		inboundsJSON = append(inboundsJSON, inboundJSON)
	}
	return inboundsJSON, nil
}
func Restart(tx *gorm.DB, ids []uint, core Core, hooks UserHooks) error {
	if core == nil || !core.IsRunning() {
		return nil
	}
	if _, err := runtimeprojection.ValidateReferences(tx, nil); err != nil {
		return err
	}
	view, err := loadRuntimeInboundView(tx)
	if err != nil {
		return err
	}
	type prepared struct {
		tag       string
		closeTags []string
		configs   []json.RawMessage
	}
	ready := []prepared{}
	selected := map[uint]bool{}
	for _, id := range ids {
		selected[id] = true
	}
	rootTags := map[string]bool{}
	for _, row := range view.rows {
		if selected[row.Id] {
			rootTags[view.groups.Root(row.Tag)] = true
		}
	}
	// Complete capability/serialization preflight for the whole batch before
	// the first removal. Unavailable historical rows do not mutate live state.
	for _, row := range view.rows {
		if !rootTags[row.Tag] || !view.facts[row.Tag].Available {
			continue
		}

		config, err := view.render(tx, row, hooks)
		if err != nil {
			return err
		}
		configs := []json.RawMessage{config}
		ready = append(ready, prepared{tag: row.Tag, closeTags: append([]string{row.Tag}, view.groups.Members(row.Tag)...), configs: configs})
	}

	for _, row := range ready {
		if err := core.RemoveInbound(row.tag); err != nil && err != os.ErrInvalid {
			return err
		}
		for _, tag := range row.closeTags {
			core.CloseInboundConnections(tag)
		}
		for _, config := range row.configs {

			if err := core.AddInbound(config); err != nil {
				return err
			}
		}
	}
	return nil
}

func RemoveFromCore(tags []string, core Core) error {
	if core == nil || !core.IsRunning() {
		return nil
	}
	for _, tag := range tags {
		if err := core.RemoveInbound(tag); err != nil && err != os.ErrInvalid {
			return err
		}
		core.CloseInboundConnections(tag)
	}
	return nil
}
