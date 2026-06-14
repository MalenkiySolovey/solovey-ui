package service

import (
	"encoding/json"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

type OutboundService struct{}

func (o *OutboundService) GetAll() (*[]map[string]interface{}, error) {
	db := database.GetDB()
	outbounds := []*model.Outbound{}
	err := db.Model(model.Outbound{}).Order(sortOrderClause).Scan(&outbounds).Error
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
	if err := annotateRemoteOutboundMetadata(db, data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (o *OutboundService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	var outboundsJson []json.RawMessage
	var outbounds []*model.Outbound
	err := db.Model(model.Outbound{}).Order(sortOrderClause).Scan(&outbounds).Error
	if err != nil {
		return nil, err
	}
	for _, outbound := range outbounds {
		outboundJson, err := outbound.MarshalJSON()
		if err != nil {
			return nil, err
		}
		outboundsJson = append(outboundsJson, outboundJson)
	}
	return outboundsJson, nil
}

func (s *OutboundService) Save(tx *gorm.DB, act string, data json.RawMessage) error {
	var err error

	switch act {
	case "new", "edit":
		var outbound model.Outbound
		err = outbound.UnmarshalJSON(data)
		if err != nil {
			return err
		}
		outbound.SortOrder, err = sortOrderForSave(tx, &model.Outbound{}, outbound.Id)
		if err != nil {
			return err
		}

		err = tx.Save(&outbound).Error
		if err != nil {
			return err
		}
	case "del":
		var tag string
		err = json.Unmarshal(data, &tag)
		if err != nil {
			return err
		}
		if err = removeOutboundReferencesTx(tx, tag); err != nil {
			return err
		}
		var linkedConnections []model.RemoteOutboundConnection
		if err = tx.Where("outbound_tag = ?", tag).Find(&linkedConnections).Error; err != nil {
			return err
		}
		for _, connection := range linkedConnections {
			err = tx.Model(&model.RemoteOutboundGroup{}).
				Where("outbound_enabled = ? AND id IN (SELECT group_id FROM remote_outbound_group_connections WHERE connection_id = ?)", true, connection.Id).
				Update("outbound_enabled", false).Error
			if err != nil {
				return err
			}
		}
		err = tx.Model(&model.RemoteOutboundConnection{}).
			Where("outbound_tag = ?", tag).
			Updates(map[string]any{
				"synced":      false,
				"outbound_id": nil,
			}).Error
		if err != nil {
			return err
		}
		err = tx.Where("tag = ?", tag).Delete(model.Outbound{}).Error
		if err != nil {
			return err
		}
	default:
		return common.NewErrorf("unknown action: %s", act)
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

func removeOutboundReferencesTx(tx *gorm.DB, tag string) error {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil
	}
	var outbounds []model.Outbound
	if err := tx.Find(&outbounds).Error; err != nil {
		return err
	}
	directAvailable := outboundTagExists(outbounds, "direct") && tag != "direct"
	for _, outbound := range outbounds {
		if outbound.Tag == tag || len(outbound.Options) == 0 {
			continue
		}
		options := map[string]interface{}{}
		if err := json.Unmarshal(outbound.Options, &options); err != nil {
			return err
		}
		changed := pruneOutboundTagList(options, tag, directAvailable && outbound.Type != "direct")
		if !changed {
			continue
		}
		updated, err := json.MarshalIndent(options, "", "  ")
		if err != nil {
			return err
		}
		if err := tx.Model(&model.Outbound{}).Where("id = ?", outbound.Id).Update("options", json.RawMessage(updated)).Error; err != nil {
			return err
		}
	}
	return nil
}

func pruneOutboundTagList(options map[string]interface{}, tag string, canFallbackDirect bool) bool {
	fallback := ""
	if canFallbackDirect {
		fallback = "direct"
	}
	changed := false
	raw, ok := options["outbounds"]
	if ok {
		values, ok := raw.([]interface{})
		if ok {
			next := make([]interface{}, 0, len(values))
			removed := false
			for _, value := range values {
				if value == tag {
					removed = true
					continue
				}
				next = append(next, value)
			}
			if removed {
				if len(next) == 0 && fallback != "" {
					next = append(next, fallback)
				}
				if fallback == "" && len(next) > 0 {
					fallback, _ = next[0].(string)
				}
				options["outbounds"] = next
				changed = true
			}
		}
	}

	for _, key := range []string{"default", "detour"} {
		if value, ok := options[key].(string); ok && value == tag {
			if fallback != "" {
				options[key] = fallback
			} else {
				delete(options, key)
			}
			changed = true
		}
	}
	return changed
}

func outboundTagExists(outbounds []model.Outbound, tag string) bool {
	for _, outbound := range outbounds {
		if outbound.Tag == tag {
			return true
		}
	}
	return false
}
