package outbounds

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityidentity "github.com/MalenkiySolovey/solovey-ui/internal/entities/identity"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/saveidentity"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/tagrefs"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

type Core interface {
	IsRunning() bool
	RemoveOutbound(tag string) error
	AddOutbound(config []byte) error
}

func GetAll(db *gorm.DB) (*[]map[string]interface{}, error) {
	outbounds := []*model.Outbound{}
	err := db.Model(model.Outbound{}).Order(entityorder.Clause).Scan(&outbounds).Error
	if err != nil {
		return nil, err
	}
	var data []map[string]interface{}
	for _, outbound := range outbounds {
		outData := map[string]interface{}{
			"id":        outbound.Id,
			"sortOrder": outbound.SortOrder,
			"type":      outbound.Type,
			"tag":       outbound.Tag,
		}
		if outbound.Options != nil {
			var restFields map[string]json.RawMessage
			if err := json.Unmarshal(outbound.Options, &restFields); err != nil {
				return nil, err
			}
			for k, v := range restFields {
				outData[k] = v
			}
		}
		data = append(data, outData)
	}
	if err := annotateMetadata(db, data); err != nil {
		return nil, err
	}
	return &data, nil
}

func GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	var outboundsJSON []json.RawMessage
	var rows []*model.Outbound
	err := db.Model(model.Outbound{}).Order(entityorder.Clause).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	directTag := DirectFallbackTag(db)
	failoverRejectSupportAdded := false
	for _, outbound := range rows {
		if outbound.Type == FailoverType {
			configs, err := AssembleFailoverOutboundsForCore(*outbound, directTag)
			if err != nil {
				return nil, err
			}
			for _, config := range configs {
				if isFailoverRejectSupportConfig(config) {
					if failoverRejectSupportAdded {
						continue
					}
					failoverRejectSupportAdded = true
				}
				outboundsJSON = append(outboundsJSON, config)
			}
			continue
		} else {
			outboundJSON, err := outbound.MarshalJSON()
			if err != nil {
				return nil, err
			}
			outboundsJSON = append(outboundsJSON, outboundJSON)
		}
	}
	return outboundsJSON, nil
}

func ValidateStored(db *gorm.DB) error {
	if db == nil {
		return common.NewError("outbound persistence is unavailable")
	}
	if !db.Migrator().HasTable(&model.Outbound{}) {
		return nil
	}
	var rows []model.Outbound
	if err := db.Order("id").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if err := jsonvalue.OptionalObject("outbound options", row.Options); err != nil {
			return fmt.Errorf("stored outbound row %d: %w", row.Id, err)
		}
		if row.Type == FailoverType {
			if err := validateFailoverGroup(db, row); err != nil {
				return fmt.Errorf("stored outbound row %d: %w", row.Id, err)
			}
		}
	}
	return nil
}

func Save(tx *gorm.DB, act string, data json.RawMessage) (*singboxapply.Change, error) {
	switch act {
	case "new", "edit":
		return saveUpsert(tx, act, data)
	case "del":
		return saveDelete(tx, data)
	default:
		return nil, common.NewErrorf("unknown action: %s", act)
	}
}

func saveUpsert(tx *gorm.DB, action string, data json.RawMessage) (*singboxapply.Change, error) {
	outbound, err := decodeSaveOutbound(data)
	if err != nil {
		return nil, err
	}
	if err := saveidentity.Validate(tx, action, outbound.Id, &model.Outbound{}); err != nil {
		return nil, err
	}
	if err := entityidentity.ValidateTypeTag(outbound.Type, outbound.Tag); err != nil {
		return nil, err
	}
	if err := entityidentity.EnsureOutboundTagAvailable(tx, outbound.Tag, outbound.Id, 0); err != nil {
		return nil, err
	}
	if outbound.Type == FailoverType {
		if err := validateFailoverGroup(tx, outbound); err != nil {
			return nil, err
		}
	}
	oldTag, err := tagByID(tx, outbound.Id)
	if err != nil {
		return nil, err
	}
	renamed := oldTag != "" && oldTag != outbound.Tag
	if renamed {
		refs, err := tagrefs.Outbound(tx, oldTag, outbound.Id, 0)
		if err != nil {
			return nil, err
		}
		if len(refs) > 0 {
			return nil, tagrefs.FormatError("outbound", oldTag, refs)
		}
	}

	outbound.SortOrder, err = entityorder.ForSave(tx, &model.Outbound{}, outbound.Id)
	if err != nil {
		return nil, err
	}
	if err := tx.Save(&outbound).Error; err != nil {
		return nil, err
	}
	if outbound.Type == FailoverType {
		return &singboxapply.Change{
			NeedsRestart:  true,
			RestartReason: fmt.Sprintf("failover group %q is assembled as a selector", outbound.Tag),
		}, nil
	}

	refs, err := tagrefs.Outbound(tx, outbound.Tag, outbound.Id, 0)
	if err != nil {
		return nil, err
	}
	if eager := tagrefs.Eager(refs); len(eager) > 0 {
		return &singboxapply.Change{
			NeedsRestart:  true,
			RestartReason: fmt.Sprintf("outbound %q is captured at construction by %s", outbound.Tag, eager[0].Locator),
		}, nil
	}
	change := &singboxapply.Change{ReloadIDs: []uint{outbound.Id}}
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
	err := tx.Model(model.Outbound{}).Select("tag").Where("id = ?", id).Find(&tag).Error
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
	refs, err := tagrefs.Outbound(tx, tag, ownID, 0)
	if err != nil {
		return nil, err
	}
	if len(refs) > 0 {
		return nil, tagrefs.FormatError("outbound", tag, refs)
	}
	if err := runDeleteHooks(tx, tag); err != nil {
		return nil, err
	}
	if err := tx.Where("tag = ?", tag).Delete(model.Outbound{}).Error; err != nil {
		return nil, err
	}
	return &singboxapply.Change{RemoveTags: []string{tag}}, nil
}

func IDByTag(tx *gorm.DB, tag string) (uint, error) {
	var id uint
	err := tx.Model(model.Outbound{}).Select("id").Where("tag = ?", tag).Scan(&id).Error
	return id, err
}

func Restart(tx *gorm.DB, ids []uint, core Core) error {
	if core == nil || !core.IsRunning() {
		return nil
	}
	var rows []*model.Outbound
	if err := tx.Model(model.Outbound{}).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return err
	}
	failoverRejectSupportAdded := false
	for _, outbound := range rows {
		if err := core.RemoveOutbound(outbound.Tag); err != nil && err != os.ErrInvalid {
			return err
		}
		var outboundConfigs []json.RawMessage
		if outbound.Type == FailoverType {
			configs, err := AssembleFailoverOutboundsForCore(*outbound, DirectFallbackTag(tx))
			if err != nil {
				return err
			}
			outboundConfigs = configs
		} else {
			config, err := outbound.MarshalJSON()
			if err != nil {
				return err
			}
			outboundConfigs = []json.RawMessage{config}
		}
		for _, outboundConfig := range outboundConfigs {
			if isFailoverRejectSupportConfig(outboundConfig) {
				if failoverRejectSupportAdded {
					continue
				}
				failoverRejectSupportAdded = true
			}
			if err := core.AddOutbound(outboundConfig); err != nil {
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
		if err := core.RemoveOutbound(tag); err != nil && err != os.ErrInvalid {
			return err
		}
	}
	return nil
}
