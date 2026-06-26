package remote

import (
	"encoding/json"
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"gorm.io/gorm"
)

type RefreshResult struct {
	SubscriptionId uint `json:"subscriptionId"`
	Fetched        int  `json:"fetched"`
	Created        int  `json:"created"`
	Updated        int  `json:"updated"`
	MarkedMissing  int  `json:"markedMissing"`
	Synced         int  `json:"synced"`
}

func MarkRefreshError(tx *gorm.DB, subscriptionID uint, message string, now int64) error {
	return tx.Model(&model.RemoteOutboundSubscription{}).
		Where("id = ?", subscriptionID).
		Updates(map[string]any{
			"last_error": message,
			"updated_at": now,
		}).Error
}

func RefreshFetchedSubscription(tx *gorm.DB, subscription *model.RemoteOutboundSubscription, fetched *FetchedSubscription, now int64) (RefreshResult, bool, error) {
	if fetched == nil {
		return RefreshResult{SubscriptionId: subscription.Id}, false, nil
	}
	collectionSnapshot, err := refreshCollectionSnapshot(fetched)
	if err != nil {
		return RefreshResult{SubscriptionId: subscription.Id}, false, err
	}
	return refreshSubscriptionOutbounds(tx, subscription, fetched.Outbounds, fetched.Snapshot, collectionSnapshot, now)
}

func RefreshSubscriptionOutbounds(tx *gorm.DB, subscription *model.RemoteOutboundSubscription, outbounds []map[string]interface{}, now int64) (RefreshResult, bool, error) {
	canonicalSnapshot := subcanonical.ObserveOutbounds(subcanonical.FormatSingBox, outbounds)
	return refreshSubscriptionOutbounds(tx, subscription, outbounds, canonicalSnapshot, nil, now)
}

func refreshSubscriptionOutbounds(tx *gorm.DB, subscription *model.RemoteOutboundSubscription, outbounds []map[string]interface{}, canonicalSnapshot subcanonical.Snapshot, collectionSnapshot json.RawMessage, now int64) (RefreshResult, bool, error) {
	result := RefreshResult{
		SubscriptionId: subscription.Id,
		Fetched:        len(outbounds),
	}
	if _, err := ReconcileOutboundLinks(tx); err != nil {
		return result, false, err
	}
	if err := ReconcileGroupStates(tx); err != nil {
		return result, false, err
	}
	if subscription.TagPrefix == "" {
		subscription.TagPrefix = DefaultTagPrefix(subscription.Name, subscription.Id)
	}
	defaultGroup, err := EnsureDefaultGroup(tx, subscription.Id, now)
	if err != nil {
		return result, false, err
	}
	if err := BackfillSubscriptionGroupMemberships(tx, subscription.Id); err != nil {
		return result, false, err
	}

	seen, syncedChanged, err := refreshSeenConnections(tx, subscription, defaultGroup.Id, outbounds, canonicalSnapshot, now, &result)
	if err != nil {
		return result, false, err
	}
	if err := ReconcileDerivedGroupDependencies(tx, subscription.Id, defaultGroup.Id); err != nil {
		return result, false, err
	}
	missingChanged, err := markMissingConnections(tx, subscription.Id, seen, now, &result)
	if err != nil {
		return result, false, err
	}
	canonicalSnapshotData, err := json.Marshal(canonicalSnapshot)
	if err != nil {
		return result, false, err
	}
	updates := map[string]any{
		"tag_prefix":         subscription.TagPrefix,
		"last_updated":       now,
		"last_error":         "",
		"canonical_snapshot": canonicalSnapshotData,
		"updated_at":         now,
	}
	if len(collectionSnapshot) > 0 {
		updates["collection_snapshot"] = collectionSnapshot
	}
	if err := tx.Model(&model.RemoteOutboundSubscription{}).Where("id = ?", subscription.Id).Updates(updates).Error; err != nil {
		return result, false, err
	}
	return result, syncedChanged || missingChanged, nil
}

func refreshCollectionSnapshot(fetched *FetchedSubscription) (json.RawMessage, error) {
	if fetched == nil {
		return nil, nil
	}
	snapshot := CollectionSnapshot{
		Formats:  fetched.Formats,
		Attempts: fetched.Attempts,
	}
	if len(snapshot.Formats) == 0 && len(snapshot.Attempts) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func refreshSeenConnections(tx *gorm.DB, subscription *model.RemoteOutboundSubscription, defaultGroupID uint, outbounds []map[string]interface{}, canonicalSnapshot subcanonical.Snapshot, now int64, result *RefreshResult) (map[string]struct{}, bool, error) {
	seen := make(map[string]struct{}, len(outbounds))
	syncedChanged := false
	for index, outbound := range outbounds {
		connection, wasSynced, err := upsertRefreshConnection(tx, subscription, defaultGroupID, outbound, canonicalConnectionAt(canonicalSnapshot, index), index, seen, now, result)
		if err != nil {
			return nil, false, err
		}
		shouldSync, err := ConnectionUsesOutboundEnabledGroup(tx, connection.Id)
		if err != nil {
			return nil, false, err
		}
		if (connection.Synced || shouldSync) && connection.Enabled && !connection.Missing {
			if err := SyncConnectionToOutbound(tx, &connection, false); err != nil {
				return nil, false, err
			}
			if err := tx.Save(&connection).Error; err != nil {
				return nil, false, err
			}
			result.Synced++
			syncedChanged = syncedChanged || !wasSynced
		}
	}
	return seen, syncedChanged, nil
}

func upsertRefreshConnection(tx *gorm.DB, subscription *model.RemoteOutboundSubscription, defaultGroupID uint, outbound map[string]interface{}, canonicalConnection *subcanonical.Connection, index int, seen map[string]struct{}, now int64, result *RefreshResult) (model.RemoteOutboundConnection, bool, error) {
	normalized, err := NormalizeOutboundWithCanonical(outbound, index, canonicalConnection)
	if err != nil {
		return model.RemoteOutboundConnection{}, false, err
	}
	normalized.SourceKey = UniqueSourceKey(seen, normalized.SourceKey)
	seen[normalized.SourceKey] = struct{}{}

	var connection model.RemoteOutboundConnection
	err = tx.Where("subscription_id = ? AND source_key = ?", subscription.Id, normalized.SourceKey).First(&connection).Error
	switch {
	case err == nil:
		if UpdateConnection(&connection, normalized, now) {
			result.Updated++
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		legacy, err := findConnectionByLegacySourceKey(tx, subscription.Id, normalized)
		if err != nil {
			return model.RemoteOutboundConnection{}, false, err
		}
		if legacy != nil {
			connection = *legacy
			if connection.SourceKey != normalized.SourceKey {
				connection.SourceKey = normalized.SourceKey
				result.Updated++
			}
			if UpdateConnection(&connection, normalized, now) {
				result.Updated++
			}
		} else {
			tag, err := UniqueOutboundTag(tx, *subscription, normalized.Name, 0)
			if err != nil {
				return model.RemoteOutboundConnection{}, false, err
			}
			sortOrder := normalized.SortOrder
			if sortOrder <= 0 {
				nextSortOrder, err := entityorder.Next(tx, &model.RemoteOutboundConnection{})
				if err != nil {
					return model.RemoteOutboundConnection{}, false, err
				}
				sortOrder = nextSortOrder
			}
			connection = model.RemoteOutboundConnection{
				SubscriptionId: subscription.Id,
				GroupId:        defaultGroupID,
				SortOrder:      sortOrder,
				Name:           normalized.Name,
				SourceKey:      normalized.SourceKey,
				Type:           normalized.Type,
				OutboundTag:    tag,
				Enabled:        true,
				Missing:        false,
				Synced:         false,
				Options:        normalized.Options,
				Canonical:      normalized.Canonical,
				LastSeen:       now,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			result.Created++
		}
	default:
		return model.RemoteOutboundConnection{}, false, err
	}

	wasSynced := connection.Synced
	if err := tx.Save(&connection).Error; err != nil {
		return model.RemoteOutboundConnection{}, false, err
	}
	if connection.GroupId != 0 {
		if err := EnsureConnectionHasGroup(tx, connection.Id, defaultGroupID); err != nil {
			return model.RemoteOutboundConnection{}, false, err
		}
	}
	if err := SyncConnectionLegacyGroupID(tx, connection.Id); err != nil {
		return model.RemoteOutboundConnection{}, false, err
	}
	return connection, wasSynced, nil
}

func findConnectionByLegacySourceKey(tx *gorm.DB, subscriptionID uint, normalized NormalizedOutbound) (*model.RemoteOutboundConnection, error) {
	legacyKey := legacyTypedLabelSourceKey(normalized.Type, normalized.SourceKey)
	if legacyKey == "" || legacyKey == normalized.SourceKey {
		return nil, nil
	}
	var connection model.RemoteOutboundConnection
	err := tx.Where("subscription_id = ? AND source_key = ?", subscriptionID, legacyKey).First(&connection).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &connection, nil
}

func canonicalConnectionAt(snapshot subcanonical.Snapshot, index int) *subcanonical.Connection {
	if index < 0 || index >= len(snapshot.Connections) {
		return nil
	}
	return &snapshot.Connections[index]
}

func markMissingConnections(tx *gorm.DB, subscriptionID uint, seen map[string]struct{}, now int64, result *RefreshResult) (bool, error) {
	var existing []model.RemoteOutboundConnection
	if err := tx.Where("subscription_id = ? AND missing = ?", subscriptionID, false).Find(&existing).Error; err != nil {
		return false, err
	}
	syncedChanged := false
	for _, connection := range existing {
		if _, ok := seen[connection.SourceKey]; ok {
			continue
		}
		connection.Missing = true
		connection.MissingReason = "not present in latest successful refresh"
		if connection.MissingSince == 0 {
			connection.MissingSince = now
		}
		connection.UpdatedAt = now
		if connection.Synced {
			if err := MarkConnectionOutboundMissing(tx, &connection, connection.MissingReason, now); err != nil {
				return false, err
			}
			syncedChanged = true
		}
		if err := tx.Save(&connection).Error; err != nil {
			return false, err
		}
		result.MarkedMissing++
	}
	return syncedChanged, nil
}

func DueSubscriptions(tx *gorm.DB, now int64) ([]model.RemoteOutboundSubscription, error) {
	var subscriptions []model.RemoteOutboundSubscription
	err := tx.
		Where("enabled = ? AND auto_update = ? AND update_interval > 0", true, true).
		Where("last_updated = 0 OR last_updated + update_interval <= ?", now).
		Order(entityorder.Clause).
		Find(&subscriptions).Error
	return subscriptions, err
}
