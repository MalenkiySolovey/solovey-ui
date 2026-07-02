//go:build !minimal

package remote

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

func AnnotateManagedOutboundMetadata(tx *gorm.DB, outbounds []map[string]interface{}) error {
	if len(outbounds) == 0 {
		return nil
	}
	ids, tags, byID, byTag := outboundMetadataTargets(outbounds)
	if err := annotateMissingOutboundMetadata(tx, ids, tags, byID, byTag); err != nil {
		return err
	}
	if !tx.Migrator().HasTable(&RemoteOutboundConnection{}) ||
		!tx.Migrator().HasTable(&RemoteOutboundSubscription{}) ||
		!tx.Migrator().HasTable(&RemoteOutboundGroupConnection{}) ||
		!tx.Migrator().HasTable(&RemoteOutboundGroup{}) {
		return nil
	}
	return annotateManagedOutboundMetadata(tx, ids, tags, byID, byTag)
}

func outboundMetadataTargets(outbounds []map[string]interface{}) ([]uint, []string, map[uint]map[string]interface{}, map[string]map[string]interface{}) {
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
	return ids, tags, byID, byTag
}

func annotateMissingOutboundMetadata(tx *gorm.DB, ids []uint, tags []string, byID map[uint]map[string]interface{}, byTag map[string]map[string]interface{}) error {
	var rows []model.Outbound
	if err := tx.Model(model.Outbound{}).
		Where("remote_missing = ?", true).
		Where("id IN ? OR tag IN ?", ids, tags).
		Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		target := byID[row.Id]
		if target == nil {
			target = byTag[row.Tag]
		}
		if target == nil {
			continue
		}
		appendComponentBadge(target, map[string]string{
			"labelKey":       "remoteOutbound.missing",
			"color":          "warning",
			"variant":        "warning",
			"vuetifyVariant": "flat",
		})
		target["componentNotice"] = firstNonEmpty(row.RemoteMissingSource, row.RemoteMissingReason)
	}
	return nil
}

func annotateManagedOutboundMetadata(tx *gorm.DB, ids []uint, tags []string, byID map[uint]map[string]interface{}, byTag map[string]map[string]interface{}) error {
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
		appendComponentBadge(target, map[string]string{
			"labelKey":       "remoteOutbound.managedOutbound",
			"color":          "info",
			"variant":        "secondary",
			"vuetifyVariant": "tonal",
		})
		target["componentDeleteHintKey"] = "remoteOutbound.deleteManagedOutboundWarning"
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

func appendComponentBadge(target map[string]interface{}, badge map[string]string) {
	if target == nil {
		return
	}
	existing, _ := target["componentBadges"].([]map[string]string)
	for _, item := range existing {
		if item["labelKey"] == badge["labelKey"] && item["label"] == badge["label"] {
			return
		}
	}
	target["componentBadges"] = append(existing, badge)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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
