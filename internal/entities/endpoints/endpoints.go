package endpoints

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
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
				epData[k] = v
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
		endpointJSON, err := endpoint.MarshalJSON()
		if err != nil {
			return nil, err
		}
		endpointsJSON = append(endpointsJSON, endpointJSON)
	}
	return endpointsJSON, nil
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
			if err := tx.Model(model.Endpoint{}).Select("json_extract(ext, '$.license_key')").Where("id = ?", endpoint.Id).Find(&oldLicense).Error; err != nil {
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
		refs, err := tagrefs.Outbound(tx, oldTag, 0, endpoint.Id)
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

	refs, err := tagrefs.Outbound(tx, endpoint.Tag, 0, endpoint.Id)
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
	ownID, err := IDByTag(tx, tag)
	if err != nil {
		return nil, err
	}
	refs, err := tagrefs.Outbound(tx, tag, 0, ownID)
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
	var rows []*model.Endpoint
	if err := tx.Model(model.Endpoint{}).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return err
	}
	for _, endpoint := range rows {
		if err := core.RemoveEndpoint(endpoint.Tag); err != nil && err != os.ErrInvalid {
			return err
		}
		endpointConfig, err := endpoint.MarshalJSON()
		if err != nil {
			return err
		}
		if err := core.AddEndpoint(endpointConfig); err != nil {
			return err
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
