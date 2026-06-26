package outbounds

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
			"id":                  outbound.Id,
			"sortOrder":           outbound.SortOrder,
			"type":                outbound.Type,
			"tag":                 outbound.Tag,
			"remoteMissing":       outbound.RemoteMissing,
			"remoteMissingReason": outbound.RemoteMissingReason,
			"remoteMissingSince":  outbound.RemoteMissingSince,
			"remoteMissingSource": outbound.RemoteMissingSource,
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
	if err := annotateRemoteOutboundMetadata(db, data); err != nil {
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
	for _, outbound := range rows {
		var outboundJSON json.RawMessage
		if outbound.Type == FailoverType {
			outboundJSON, err = AssembleFailoverForCore(*outbound, directTag)
		} else {
			outboundJSON, err = outbound.MarshalJSON()
		}
		if err != nil {
			return nil, err
		}
		outboundsJSON = append(outboundsJSON, outboundJSON)
	}
	return outboundsJSON, nil
}

func Save(tx *gorm.DB, act string, data json.RawMessage) (*singboxapply.Change, error) {
	switch act {
	case "new", "edit":
		return saveUpsert(tx, data)
	case "del":
		return saveDelete(tx, data)
	default:
		return nil, common.NewErrorf("unknown action: %s", act)
	}
}

func saveUpsert(tx *gorm.DB, data json.RawMessage) (*singboxapply.Change, error) {
	var outbound model.Outbound
	if err := outbound.UnmarshalJSON(data); err != nil {
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
	if err := UnsyncRemoteConnections(tx, tag); err != nil {
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

func UnsyncRemoteConnections(tx *gorm.DB, tag string) error {
	var linkedConnections []model.RemoteOutboundConnection
	if err := tx.Where("outbound_tag = ?", tag).Find(&linkedConnections).Error; err != nil {
		return err
	}
	for _, connection := range linkedConnections {
		if err := tx.Model(&model.RemoteOutboundGroup{}).
			Where("outbound_enabled = ? AND id IN (SELECT group_id FROM remote_outbound_group_connections WHERE connection_id = ?)", true, connection.Id).
			Update("outbound_enabled", false).Error; err != nil {
			return err
		}
	}
	return tx.Model(&model.RemoteOutboundConnection{}).
		Where("outbound_tag = ?", tag).
		Updates(map[string]any{
			"synced":      false,
			"outbound_id": nil,
		}).Error
}

func Restart(tx *gorm.DB, ids []uint, core Core) error {
	if core == nil || !core.IsRunning() {
		return nil
	}
	var rows []*model.Outbound
	if err := tx.Model(model.Outbound{}).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return err
	}
	for _, outbound := range rows {
		if err := core.RemoveOutbound(outbound.Tag); err != nil && err != os.ErrInvalid {
			return err
		}
		var outboundConfig json.RawMessage
		var err error
		if outbound.Type == FailoverType {
			outboundConfig, err = AssembleFailoverForCore(*outbound, DirectFallbackTag(tx))
		} else {
			outboundConfig, err = outbound.MarshalJSON()
		}
		if err != nil {
			return err
		}
		if err := core.AddOutbound(outboundConfig); err != nil {
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
		if err := core.RemoveOutbound(tag); err != nil && err != os.ErrInvalid {
			return err
		}
	}
	return nil
}

func annotateRemoteOutboundMetadata(tx *gorm.DB, outbounds []map[string]interface{}) error {
	if len(outbounds) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(outbounds))
	tags := make([]string, 0, len(outbounds))
	byID := map[uint]map[string]interface{}{}
	byTag := map[string]map[string]interface{}{}
	for index := range outbounds {
		id := uintFromInterface(outbounds[index]["id"])
		tag, _ := outbounds[index]["tag"].(string)
		if id != 0 {
			ids = append(ids, id)
			byID[id] = outbounds[index]
		}
		if tag != "" {
			tags = append(tags, tag)
			byTag[tag] = outbounds[index]
		}
	}
	var rows []struct {
		OutboundId       *uint
		OutboundTag      string
		ConnectionName   string
		SubscriptionName string
		GroupName        string
	}
	if err := tx.Table("remote_outbound_connections").
		Select("remote_outbound_connections.outbound_id, remote_outbound_connections.outbound_tag, remote_outbound_connections.name AS connection_name, remote_outbound_subscriptions.name AS subscription_name, remote_outbound_groups.name AS group_name").
		Joins("LEFT JOIN remote_outbound_subscriptions ON remote_outbound_subscriptions.id = remote_outbound_connections.subscription_id").
		Joins("LEFT JOIN remote_outbound_group_connections ON remote_outbound_group_connections.connection_id = remote_outbound_connections.id").
		Joins("LEFT JOIN remote_outbound_groups ON remote_outbound_groups.id = remote_outbound_group_connections.group_id").
		Where("remote_outbound_connections.synced = ?", true).
		Where("remote_outbound_connections.outbound_id IN ? OR remote_outbound_connections.outbound_tag IN ?", ids, tags).
		Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		target := map[string]interface{}(nil)
		if row.OutboundId != nil {
			target = byID[*row.OutboundId]
		}
		if target == nil {
			target = byTag[row.OutboundTag]
		}
		if target == nil {
			continue
		}
		target["remoteOutboundManaged"] = true
		if row.ConnectionName != "" {
			target["remoteOutboundConnection"] = row.ConnectionName
		}
		if row.SubscriptionName != "" {
			target["remoteOutboundSubscription"] = row.SubscriptionName
		}
		if row.GroupName != "" {
			target["remoteOutboundGroups"] = appendUniqueStringInterface(target["remoteOutboundGroups"], row.GroupName)
		}
	}
	return nil
}

func uintFromInterface(value interface{}) uint {
	switch v := value.(type) {
	case uint:
		return v
	case int:
		if v > 0 {
			return uint(v)
		}
	case int64:
		if v > 0 {
			return uint(v)
		}
	case float64:
		if v > 0 {
			return uint(v)
		}
	}
	return 0
}

func appendUniqueStringInterface(value interface{}, next string) []string {
	existing, _ := value.([]string)
	for _, item := range existing {
		if item == next {
			return existing
		}
	}
	return append(existing, next)
}
