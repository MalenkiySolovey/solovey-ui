package endpoints

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityidentity "github.com/MalenkiySolovey/solovey-ui/internal/entities/identity"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	runtimeprojection "github.com/MalenkiySolovey/solovey-ui/internal/entities/runtimeprojection"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/saveeligibility"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/tagrefs"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

type Core interface {
	IsRunning() bool
	RemoveEndpoint(tag string) error
	AddEndpoint(config []byte) error
}

type WarpHooks interface {
	RegisterWarp(ep *model.Endpoint) error
	SetWarpLicense(oldLicense string, ep *model.Endpoint) error
}

func GetAll(db *gorm.DB) (*[]map[string]interface{}, error) {
	endpoints := []*model.Endpoint{}
	err := db.Model(model.Endpoint{}).Order(entityorder.Clause).Scan(&endpoints).Error
	if err != nil {
		return nil, err
	}
	var data []map[string]interface{}
	for _, endpoint := range endpoints {
		epData := map[string]interface{}{
			"id":        endpoint.Id,
			"sortOrder": endpoint.SortOrder,
			"type":      endpoint.Type,
			"tag":       endpoint.Tag,
			"ext":       endpoint.Ext,
		}
		if endpoint.Options != nil {
			var restFields map[string]json.RawMessage
			if err := json.Unmarshal(endpoint.Options, &restFields); err != nil {
				return nil, err
			}
			for k, v := range restFields {
				if _, fixed := epData[k]; !fixed {
					epData[k] = v
				}
			}
		}
		data = append(data, epData)
	}
	return &data, nil
}

func GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	var endpointsJSON []json.RawMessage
	var rows []*model.Endpoint
	err := db.Model(model.Endpoint{}).Order(entityorder.Clause).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, endpoint := range rows {
		if !runtimeprojection.Included("endpoints", endpoint.Type) {
			continue
		}
		endpointJSON, err := endpoint.MarshalJSON()
		if err != nil {
			return nil, err
		}
		endpointsJSON = append(endpointsJSON, endpointJSON)
	}
	return endpointsJSON, nil
}

func ValidateStored(db *gorm.DB) error {
	if db == nil {
		return common.NewError("endpoint persistence is unavailable")
	}
	if !db.Migrator().HasTable(&model.Endpoint{}) {
		return nil
	}
	var rows []model.Endpoint
	if err := db.Order("id").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if err := jsonvalue.OptionalObject("endpoint options", row.Options); err != nil {
			return fmt.Errorf("stored endpoint row %d: %w", row.Id, err)
		}
		if err := jsonvalue.OptionalObject("endpoint extension", row.Ext); err != nil {
			return fmt.Errorf("stored endpoint row %d: %w", row.Id, err)
		}
	}
	return nil
}

func Save(tx *gorm.DB, act string, data json.RawMessage, warp WarpHooks) (*singboxapply.Change, error) {
	switch act {
	case "new", "edit":
		return saveUpsert(tx, act, data, warp)
	case "del":
		return saveDelete(tx, data)
	default:
		return nil, common.NewErrorf("unknown action: %s", act)
	}
}

func saveUpsert(tx *gorm.DB, act string, data json.RawMessage, warp WarpHooks) (*singboxapply.Change, error) {
	var endpoint model.Endpoint
	if err := endpoint.UnmarshalJSON(data); err != nil {
		return nil, err
	}
	if err := saveeligibility.Validate(tx, act, endpoint.Id, &model.Endpoint{}, "endpoints", endpoint.Type); err != nil {
		return nil, err
	}
	if err := entityidentity.ValidateTypeTag(endpoint.Type, endpoint.Tag); err != nil {
		return nil, err
	}
	if err := entityidentity.EnsureOutboundTagAvailable(tx, endpoint.Tag, 0, endpoint.Id); err != nil {
		return nil, err
	}

	if endpoint.Type == "warp" {
		if warp == nil {
			return nil, common.NewError("warp endpoint hook is not configured")
		}
		if act == "new" {
			if err := warp.RegisterWarp(&endpoint); err != nil {
				return nil, err
			}
		} else {
			var oldLicense string
			if err := tx.Model(model.Endpoint{}).Select("COALESCE(json_extract(ext, '$.license_key'), '')").Where("id = ?", endpoint.Id).Find(&oldLicense).Error; err != nil {
				return nil, err
			}
			if err := warp.SetWarpLicense(oldLicense, &endpoint); err != nil {
				return nil, err
			}
		}
	}

	oldTag, err := tagByID(tx, endpoint.Id)
	if err != nil {
		return nil, err
	}
	renamed := oldTag != "" && oldTag != endpoint.Tag
	if renamed {
		refs, err := tagrefs.Endpoint(tx, oldTag, endpoint.Id)
		if err != nil {
			return nil, err
		}
		if len(refs) > 0 {
			return nil, tagrefs.FormatError("endpoint", oldTag, refs)
		}
	}

	endpoint.SortOrder, err = entityorder.ForSave(tx, &model.Endpoint{}, endpoint.Id)
	if err != nil {
		return nil, err
	}
	if err := tx.Save(&endpoint).Error; err != nil {
		return nil, err
	}

	refs, err := tagrefs.Endpoint(tx, endpoint.Tag, endpoint.Id)
	if err != nil {
		return nil, err
	}
	if eager := tagrefs.Eager(refs); len(eager) > 0 {
		return &singboxapply.Change{
			NeedsRestart:  true,
			RestartReason: fmt.Sprintf("endpoint %q is captured at construction by %s", endpoint.Tag, eager[0].Locator),
		}, nil
	}
	change := &singboxapply.Change{ReloadIDs: []uint{endpoint.Id}}
	if renamed {
		change.RemoveTags = []string{oldTag}
	}
	return change, nil
}

func tagByID(tx *gorm.DB, id uint) (string, error) {
	if id == 0 {
		return "", nil
	}
	var tag string
	err := tx.Model(model.Endpoint{}).Select("tag").Where("id = ?", id).Find(&tag).Error
	return tag, err
}

func saveDelete(tx *gorm.DB, data json.RawMessage) (*singboxapply.Change, error) {
	var tag string
	if err := json.Unmarshal(data, &tag); err != nil {
		return nil, err
	}
	if err := entityidentity.ValidateTag(tag); err != nil {
		return nil, err
	}
	ownID, err := IDByTag(tx, tag)
	if err != nil {
		return nil, err
	}
	refs, err := tagrefs.Endpoint(tx, tag, ownID)
	if err != nil {
		return nil, err
	}
	if len(refs) > 0 {
		return nil, tagrefs.FormatError("endpoint", tag, refs)
	}
	if err := tx.Where("tag = ?", tag).Delete(model.Endpoint{}).Error; err != nil {
		return nil, err
	}
	return &singboxapply.Change{RemoveTags: []string{tag}}, nil
}

func IDByTag(tx *gorm.DB, tag string) (uint, error) {
	var id uint
	err := tx.Model(model.Endpoint{}).Select("id").Where("tag = ?", tag).Scan(&id).Error
	return id, err
}

func Restart(tx *gorm.DB, ids []uint, core Core) error {
	if core == nil || !core.IsRunning() {
		return nil
	}
	if _, err := runtimeprojection.ValidateReferences(tx, nil); err != nil {
		return err
	}
	var rows []*model.Endpoint
	if err := tx.Model(model.Endpoint{}).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return err
	}
	type prepared struct {
		tag     string
		configs []json.RawMessage
	}
	ready := []prepared{}
	// Complete capability/serialization preflight for the whole batch before
	// the first removal. Unavailable historical rows do not mutate live state.
	for _, row := range rows {
		if !runtimeprojection.Included("endpoints", row.Type) {
			continue
		}
		refs, err := tagrefs.Endpoint(tx, row.Tag, row.Id)
		if err != nil {
			return err
		}
		if eager := tagrefs.Eager(refs); len(eager) > 0 {
			return &tagrefs.ReloadRejection{Tag: row.Tag, References: eager}
		}
		config, err := row.MarshalJSON()
		if err != nil {
			return err
		}
		configs := []json.RawMessage{config}
		ready = append(ready, prepared{row.Tag, configs})
	}

	for _, row := range ready {
		if err := core.RemoveEndpoint(row.tag); err != nil && err != os.ErrInvalid {
			return err
		}

		for _, config := range row.configs {

			if err := core.AddEndpoint(config); err != nil {
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
		if err := core.RemoveEndpoint(tag); err != nil && err != os.ErrInvalid {
			return err
		}
	}
	return nil
}
