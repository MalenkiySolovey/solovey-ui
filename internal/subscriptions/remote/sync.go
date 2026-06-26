package remote

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/tagrefs"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

func SyncConnectionToOutbound(tx *gorm.DB, connection *model.RemoteOutboundConnection, requireUsable bool) error {
	return syncConnectionToOutbound(tx, connection, requireUsable, map[uint]struct{}{})
}
func syncConnectionToOutbound(tx *gorm.DB, connection *model.RemoteOutboundConnection, requireUsable bool, visited map[uint]struct{}) error {
	if connection == nil {
		return common.NewError("connection is required")
	}
	if connection.Id != 0 {
		if _, ok := visited[connection.Id]; ok {
			return nil
		}
		visited[connection.Id] = struct{}{}
	}
	if remoteConnectionIsGroup(*connection) {
		if err := syncGroupDependencyOutbounds(tx, *connection, requireUsable, visited); err != nil {
			return err
		}
	}
	if err := syncSingleConnectionToOutbound(tx, connection, requireUsable); err != nil {
		return err
	}
	return nil
}
func syncSingleConnectionToOutbound(tx *gorm.DB, connection *model.RemoteOutboundConnection, requireUsable bool) error {
	if requireUsable {
		switch {
		case !connection.Enabled:
			return common.NewError("disabled connection can not be synced")
		case connection.Missing:
			return common.NewError("missing connection can not be synced")
		}
	}
	if strings.TrimSpace(connection.OutboundTag) == "" {
		return common.NewError("connection outbound tag is empty")
	}
	var outbound model.Outbound
	var err error
	if connection.OutboundId != nil && *connection.OutboundId != 0 {
		err = tx.First(&outbound, *connection.OutboundId).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = nil
		}
	}
	if outbound.Id == 0 && err == nil {
		err = tx.Where("tag = ?", connection.OutboundTag).First(&outbound).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			err = nil
		}
	}
	if err != nil {
		return err
	}
	if outbound.Id != 0 {
		if connection.OutboundId == nil || outbound.Id != *connection.OutboundId {
			var linked int64
			if err := tx.Model(&model.RemoteOutboundConnection{}).
				Where("outbound_id = ? AND id = ?", outbound.Id, connection.Id).
				Count(&linked).Error; err != nil {
				return err
			}
			if linked == 0 {
				return common.NewError("outbound tag already exists outside this remote connection")
			}
		}
		if connection.OutboundId != nil && outbound.Id != *connection.OutboundId {
			return common.NewError("outbound tag belongs to another outbound")
		}
		var linkedCount int64
		if err := tx.Model(&model.RemoteOutboundConnection{}).
			Where("outbound_id = ? AND id <> ?", outbound.Id, connection.Id).
			Count(&linkedCount).Error; err != nil {
			return err
		}
		if linkedCount > 0 {
			return common.NewError("outbound is already linked to another remote connection")
		}
	} else {
		sortOrder, err := entityorder.Next(tx, &model.Outbound{})
		if err != nil {
			return err
		}
		outbound.SortOrder = sortOrder
		outbound.Tag = connection.OutboundTag
	}
	config, err := connectionOutboundConfigForSync(tx, *connection)
	if err != nil {
		return err
	}
	options := map[string]any{}
	if err := json.Unmarshal(config, &options); err != nil {
		return err
	}
	delete(options, "type")
	delete(options, "tag")
	optionData, err := json.Marshal(options)
	if err != nil {
		return err
	}
	outbound.Type = connection.Type
	outbound.Options = optionData
	outbound.RemoteMissing = false
	outbound.RemoteMissingReason = ""
	outbound.RemoteMissingSince = 0
	outbound.RemoteMissingSource = ""
	if err := tx.Save(&outbound).Error; err != nil {
		return err
	}
	connection.OutboundId = &outbound.Id
	connection.Synced = true
	connection.UpdatedAt = time.Now().Unix()
	return nil
}
func MarkConnectionOutboundMissing(tx *gorm.DB, connection *model.RemoteOutboundConnection, reason string, now int64) error {
	if connection == nil {
		return nil
	}
	var outbound model.Outbound
	var err error
	if connection.OutboundId != nil && *connection.OutboundId != 0 {
		err = tx.First(&outbound, *connection.OutboundId).Error
	}
	if (connection.OutboundId == nil || *connection.OutboundId == 0 || errors.Is(err, gorm.ErrRecordNotFound)) && strings.TrimSpace(connection.OutboundTag) != "" {
		err = tx.Where("tag = ?", connection.OutboundTag).First(&outbound).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if outbound.Id == 0 {
		return nil
	}
	source := remoteMissingSource(tx, *connection)
	if strings.TrimSpace(reason) == "" {
		reason = "not present in latest successful refresh"
	}
	outbound.RemoteMissing = true
	outbound.RemoteMissingReason = reason
	outbound.RemoteMissingSince = now
	outbound.RemoteMissingSource = source
	return tx.Save(&outbound).Error
}
func remoteMissingSource(tx *gorm.DB, connection model.RemoteOutboundConnection) string {
	var subscription model.RemoteOutboundSubscription
	if connection.SubscriptionId != 0 && tx.First(&subscription, connection.SubscriptionId).Error == nil {
		if strings.TrimSpace(connection.Name) != "" {
			return strings.TrimSpace(subscription.Name + " / " + connection.Name)
		}
		return strings.TrimSpace(subscription.Name)
	}
	return strings.TrimSpace(connection.Name)
}
func connectionOutboundConfigForSync(tx *gorm.DB, connection model.RemoteOutboundConnection) (json.RawMessage, error) {
	tagMap, err := subscriptionConnectionTagMap(tx, connection.SubscriptionId)
	if err != nil {
		return nil, err
	}
	return connectionOutboundConfig(connection, tagMap)
}
func subscriptionConnectionTagMap(tx *gorm.DB, subscriptionID uint) (map[string]string, error) {
	if subscriptionID == 0 {
		return nil, nil
	}
	var connections []model.RemoteOutboundConnection
	if err := tx.Where("subscription_id = ?", subscriptionID).Find(&connections).Error; err != nil {
		return nil, err
	}
	return remoteConnectionTagMap(connections), nil
}
func remoteConnectionTagMap(connections []model.RemoteOutboundConnection) map[string]string {
	tagMap := make(map[string]string, len(connections)*2)
	for _, connection := range connections {
		outboundTag := strings.TrimSpace(connection.OutboundTag)
		if outboundTag == "" {
			continue
		}
		tagMap[outboundTag] = outboundTag
		if name := strings.TrimSpace(connection.Name); name != "" {
			tagMap[name] = outboundTag
		}
		if sourceKey := strings.TrimSpace(connection.SourceKey); sourceKey != "" {
			tagMap[sourceKey] = outboundTag
		}
	}
	if len(tagMap) == 0 {
		return nil
	}
	return tagMap
}
func rewriteOutboundTagReferences(raw map[string]any, tagMap map[string]string) {
	if len(tagMap) == 0 || raw == nil {
		return
	}
	if value, ok := raw["outbounds"]; ok {
		raw["outbounds"] = rewriteOutboundTagList(value, tagMap)
	}
	if value, ok := raw["default"].(string); ok {
		if rewritten := tagMap[strings.TrimSpace(value)]; rewritten != "" {
			raw["default"] = rewritten
		}
	}
}
func rewriteOutboundTagList(value any, tagMap map[string]string) any {
	switch typed := value.(type) {
	case []string:
		rewritten := make([]string, 0, len(typed))
		for _, tag := range typed {
			rewritten = append(rewritten, rewriteOutboundTag(tag, tagMap))
		}
		return rewritten
	case []any:
		rewritten := make([]string, 0, len(typed))
		for _, item := range typed {
			tag, ok := item.(string)
			if !ok {
				continue
			}
			rewritten = append(rewritten, rewriteOutboundTag(tag, tagMap))
		}
		return rewritten
	default:
		return value
	}
}
func rewriteOutboundTag(tag string, tagMap map[string]string) string {
	trimmed := strings.TrimSpace(tag)
	if rewritten := tagMap[trimmed]; rewritten != "" {
		return rewritten
	}
	return tag
}
func UnsyncConnectionFromOutbound(tx *gorm.DB, connection *model.RemoteOutboundConnection) error {
	now := time.Now().Unix()
	if _, err := deleteLinkedOutbound(tx, *connection); err != nil {
		return err
	}
	connection.Synced = false
	connection.OutboundId = nil
	connection.UpdatedAt = now
	return tx.Save(connection).Error
}
func DeleteSyncedOutboundsForSubscription(tx *gorm.DB, subscriptionID uint) (bool, error) {
	var connections []model.RemoteOutboundConnection
	if err := tx.
		Where("subscription_id = ? AND synced = ?", subscriptionID, true).
		Find(&connections).Error; err != nil {
		return false, err
	}
	changed := false
	for _, connection := range connections {
		deleted, err := deleteLinkedOutbound(tx, connection)
		if err != nil {
			return false, err
		}
		changed = changed || deleted
	}
	return changed, nil
}
func deleteLinkedOutbound(tx *gorm.DB, connection model.RemoteOutboundConnection) (bool, error) {
	if connection.OutboundId == nil || *connection.OutboundId == 0 {
		return false, nil
	}
	refs, err := tagrefs.Outbound(tx, connection.OutboundTag, *connection.OutboundId, 0)
	if err != nil {
		return false, err
	}
	if len(refs) > 0 {
		return false, tagrefs.FormatError("outbound", connection.OutboundTag, refs)
	}
	result := tx.Where("id = ? AND tag = ?", *connection.OutboundId, connection.OutboundTag).Delete(&model.Outbound{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}
func ReconcileOutboundLinks(tx *gorm.DB) (bool, error) {
	var connections []model.RemoteOutboundConnection
	if err := tx.Where("synced = ?", true).Find(&connections).Error; err != nil {
		return false, err
	}
	changed := false
	now := time.Now().Unix()
	for _, connection := range connections {
		if connection.OutboundId == nil || *connection.OutboundId == 0 {
			if err := markConnectionUnsynced(tx, connection.Id, now); err != nil {
				return false, err
			}
			changed = true
			continue
		}
		var outbound model.Outbound
		err := tx.First(&outbound, *connection.OutboundId).Error
		if err == nil && outbound.Tag == connection.OutboundTag {
			matches, matchErr := outboundMatchesRemoteConnection(tx, connection, outbound)
			if matchErr != nil {
				return false, matchErr
			}
			if matches {
				continue
			}
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return false, err
		}
		if err := markConnectionUnsynced(tx, connection.Id, now); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}
func outboundMatchesRemoteConnection(tx *gorm.DB, connection model.RemoteOutboundConnection, outbound model.Outbound) (bool, error) {
	config, err := connectionOutboundConfigForSync(tx, connection)
	if err != nil {
		return false, err
	}
	expected := map[string]any{}
	if err := json.Unmarshal(config, &expected); err != nil {
		return false, err
	}
	expectedType, _ := expected["type"].(string)
	delete(expected, "type")
	delete(expected, "tag")
	expectedOptions, err := json.Marshal(expected)
	if err != nil {
		return false, err
	}
	return outbound.Type == expectedType && JSONRawEqual(outbound.Options, expectedOptions), nil
}
func markConnectionUnsynced(tx *gorm.DB, connectionID uint, now int64) error {
	return tx.Model(&model.RemoteOutboundConnection{}).
		Where("id = ?", connectionID).
		Updates(map[string]any{
			"synced":      false,
			"outbound_id": nil,
			"updated_at":  now,
		}).Error
}
