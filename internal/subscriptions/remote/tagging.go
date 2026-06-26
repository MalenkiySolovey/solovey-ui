package remote

import (
	"fmt"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

func RetagSubscriptionConnections(tx *gorm.DB, subscription *model.RemoteOutboundSubscription) (bool, error) {
	if subscription == nil || subscription.Id == 0 {
		return false, nil
	}
	var connections []model.RemoteOutboundConnection
	if err := tx.
		Where("subscription_id = ?", subscription.Id).
		Order(entityorder.Clause).
		Find(&connections).Error; err != nil {
		return false, err
	}
	coreRestart := false
	now := time.Now().Unix()
	for index := range connections {
		connection := &connections[index]
		newTag, err := UniqueOutboundTag(tx, *subscription, connection.Name, connection.Id)
		if err != nil {
			return false, err
		}
		if newTag == connection.OutboundTag {
			continue
		}
		oldTag := connection.OutboundTag
		if connection.Synced && connection.OutboundId != nil && *connection.OutboundId != 0 {
			result := tx.Model(&model.Outbound{}).
				Where("id = ? AND tag = ?", *connection.OutboundId, oldTag).
				Update("tag", newTag)
			if result.Error != nil {
				return false, result.Error
			}
			if result.RowsAffected > 0 {
				coreRestart = true
			} else {
				connection.Synced = false
				connection.OutboundId = nil
			}
		}
		connection.OutboundTag = newTag
		connection.UpdatedAt = now
		if err := tx.Save(connection).Error; err != nil {
			return false, err
		}
	}
	return coreRestart, nil
}
func UniqueOutboundTag(tx *gorm.DB, subscription model.RemoteOutboundSubscription, name string, connectionID uint) (string, error) {
	prefix := SanitizeTagPrefix(subscription.TagPrefix)
	if prefix == "" {
		prefix = DefaultTagPrefix(subscription.Name, subscription.Id)
	}
	base := prefix + SanitizeTagPart(name)
	if base == "" {
		base = prefix + "connection"
	}
	base = TrimTag(base)
	for index := 0; index < 1000; index++ {
		candidate := base
		if index > 0 {
			candidate = TrimTag(fmt.Sprintf("%s-%d", base, index+1))
		}
		available, err := OutboundTagAvailable(tx, candidate, connectionID)
		if err != nil {
			return "", err
		}
		if available {
			return candidate, nil
		}
	}
	return "", common.NewError("unable to allocate unique outbound tag")
}
func OutboundTagAvailable(tx *gorm.DB, tag string, connectionID uint) (bool, error) {
	var outbounds int64
	if err := tx.Model(&model.Outbound{}).Where("tag = ?", tag).Count(&outbounds).Error; err != nil {
		return false, err
	}
	if outbounds > 0 {
		if connectionID == 0 {
			return false, nil
		}
		var linked int64
		if err := tx.Model(&model.RemoteOutboundConnection{}).
			Where("outbound_tag = ? AND id = ?", tag, connectionID).
			Count(&linked).Error; err != nil {
			return false, err
		}
		return linked > 0, nil
	}
	var connections int64
	query := tx.Model(&model.RemoteOutboundConnection{}).Where("outbound_tag = ?", tag)
	if connectionID != 0 {
		query = query.Where("id <> ?", connectionID)
	}
	if err := query.Count(&connections).Error; err != nil {
		return false, err
	}
	return connections == 0, nil
}
