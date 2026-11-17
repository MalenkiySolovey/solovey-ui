//go:build !minimal

package remote

import (
	"context"
	"encoding/json"
	"errors"
	feature "github.com/MalenkiySolovey/solovey-component-remote-outbound-subscriptions/domain"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/backendoutbounds"
	coreruntime "github.com/MalenkiySolovey/solovey-ui/core/runtime"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"gorm.io/gorm"
)

type backendStore struct{ db *gorm.DB }

func sourceStore(db *gorm.DB) feature.Store {
	if db == nil {
		return nil
	}
	return &backendStore{db}
}
func (b *backendStore) Context() context.Context { return b.db.Statement.Context }

type sourceStoreError struct{ cause error }

func (e sourceStoreError) Error() string { return e.cause.Error() }
func (e sourceStoreError) Unwrap() error { return e.cause }
func (e sourceStoreError) Is(target error) bool {
	return target == feature.ErrRecordNotFound || errors.Is(e.cause, target)
}
func sourceBackendError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return sourceStoreError{err}
	}
	return err
}

func (b *backendStore) ListGroupConnections(groupID uint, out *[]RemoteOutboundConnection) error {
	return sourceBackendError(func() error {
		return b.db.Joins("JOIN remote_outbound_group_connections ON remote_outbound_group_connections.connection_id = remote_outbound_connections.id").Where("remote_outbound_group_connections.group_id = ?", groupID).Order("remote_outbound_connections.sort_order ASC, remote_outbound_connections.id ASC").Find(out).Error
	}())
}

func (b *backendStore) ListGroupMemberships(groupID uint, out *[]RemoteOutboundGroupConnection) error {
	return sourceBackendError(func() error { return b.db.Where("group_id = ?", groupID).Find(out).Error }())
}

func (b *backendStore) CountMembership(groupID, connectionID uint, out *int64) error {
	return sourceBackendError(func() error {
		return b.db.Model(&RemoteOutboundGroupConnection{}).Where("group_id = ? AND connection_id = ?", groupID, connectionID).Count(out).Error
	}())
}

func (b *backendStore) CreateMembership(link *RemoteOutboundGroupConnection) error {
	return sourceBackendError(func() error { return b.db.Create(link).Error }())
}

func (b *backendStore) CountConnectionMemberships(connectionID uint, out *int64) error {
	return sourceBackendError(func() error {
		return b.db.Model(&RemoteOutboundGroupConnection{}).Where("connection_id = ?", connectionID).Count(out).Error
	}())
}

func (b *backendStore) ListSubscriptionIDs(out *[]RemoteOutboundSubscription) error {
	return sourceBackendError(func() error { return b.db.Select("id").Find(out).Error }())
}

func (b *backendStore) ListSubscriptionConnections(subscriptionID uint, out *[]RemoteOutboundConnection) error {
	return sourceBackendError(func() error { return b.db.Where("subscription_id = ?", subscriptionID).Find(out).Error }())
}

func (b *backendStore) CountSubscriptionGroup(subscriptionID, groupID uint, out *int64) error {
	return sourceBackendError(func() error {
		return b.db.Model(&RemoteOutboundGroup{}).Where("id = ? AND subscription_id = ?", groupID, subscriptionID).Count(out).Error
	}())
}

func (b *backendStore) ListMembershipsForConnections(connectionIDs []uint, out *[]RemoteOutboundGroupConnection) error {
	return sourceBackendError(func() error {
		return b.db.Where("connection_id IN ?", connectionIDs).Order("group_id ASC").Find(out).Error
	}())
}

func (b *backendStore) ListSubscriptionConnectionIDs(subscriptionID uint, out *[]RemoteOutboundConnection) error {
	return sourceBackendError(func() error { return b.db.Select("id").Where("subscription_id = ?", subscriptionID).Find(out).Error }())
}

func (b *backendStore) FirstConnectionGroup(connectionID uint, out *RemoteOutboundGroup) error {
	return sourceBackendError(func() error {
		return b.db.Joins("JOIN remote_outbound_group_connections ON remote_outbound_group_connections.group_id = remote_outbound_groups.id").Where("remote_outbound_group_connections.connection_id = ?", connectionID).Order("remote_outbound_groups.sort_order ASC, remote_outbound_groups.id ASC").First(out).Error
	}())
}

func (b *backendStore) SetLegacyGroup(connectionID, groupID uint) error {
	return sourceBackendError(func() error {
		return b.db.Model(&RemoteOutboundConnection{}).Where("id = ?", connectionID).Update("group_id", groupID).Error
	}())
}

func (b *backendStore) CountOutboundGroupsForConnection(connectionID uint, out *int64) error {
	return sourceBackendError(func() error {
		return b.db.Model(&RemoteOutboundGroup{}).Joins("JOIN remote_outbound_group_connections ON remote_outbound_group_connections.group_id = remote_outbound_groups.id").Where("remote_outbound_group_connections.connection_id = ? AND remote_outbound_groups.outbound_enabled = ?", connectionID, true).Count(out).Error
	}())
}

func (b *backendStore) ListOutboundEnabledGroups(out *[]RemoteOutboundGroup) error {
	return sourceBackendError(func() error { return b.db.Where("outbound_enabled = ?", true).Find(out).Error }())
}

func (b *backendStore) DisableGroupOutbounds(groupID uint, now int64) error {
	return sourceBackendError(func() error {
		return b.db.Model(&RemoteOutboundGroup{}).Where("id = ?", groupID).Updates(map[string]any{"outbound_enabled": false, "updated_at": now}).Error
	}())
}

func (b *backendStore) FindDefaultGroup(subscriptionID uint, out *RemoteOutboundGroup) error {
	return sourceBackendError(func() error {
		return b.db.Where("subscription_id = ? AND name = ?", subscriptionID, DefaultGroupName).First(out).Error
	}())
}

func (b *backendStore) NextGroupOrder() (int, error) {
	return entityorder.Next(b.db, &RemoteOutboundGroup{})
}

func (b *backendStore) CreateGroup(group *RemoteOutboundGroup) error {
	return sourceBackendError(func() error { return b.db.Create(group).Error }())
}

func (b *backendStore) LoadConnection(connectionID uint, out *RemoteOutboundConnection) error {
	return sourceBackendError(func() error { return b.db.First(out, connectionID).Error }())
}

func (b *backendStore) LoadGroup(groupID uint, out *RemoteOutboundGroup) error {
	return sourceBackendError(func() error { return b.db.First(out, groupID).Error }())
}

func (b *backendStore) SaveConnection(connection *RemoteOutboundConnection) error {
	return sourceBackendError(func() error { return b.db.Save(connection).Error }())
}

func (b *backendStore) SaveGroup(group *RemoteOutboundGroup) error {
	return sourceBackendError(func() error { return b.db.Save(group).Error }())
}

func (b *backendStore) DeleteConnectionMemberships(connectionID uint) error {
	return sourceBackendError(func() error {
		return b.db.Where("connection_id = ?", connectionID).Delete(&RemoteOutboundGroupConnection{}).Error
	}())
}

func (b *backendStore) MarkRefreshError(subscriptionID uint, message string, now int64) error {
	return sourceBackendError(func() error {
		return b.db.Model(&RemoteOutboundSubscription{}).Where("id = ?", subscriptionID).Updates(map[string]any{"last_error": message, "updated_at": now}).Error
	}())
}

func (b *backendStore) CommitRefresh(subscriptionID uint, snapshot feature.RefreshSnapshot) error {
	return sourceBackendError(func() error {
		updates := map[string]any{"tag_prefix": snapshot.TagPrefix, "last_updated": snapshot.UpdatedAt, "last_error": "", "canonical_snapshot": snapshot.Canonical, "updated_at": snapshot.UpdatedAt}
		if len(snapshot.Collection) > 0 {
			updates["collection_snapshot"] = snapshot.Collection
		}
		return b.db.Model(&RemoteOutboundSubscription{}).Where("id = ?", subscriptionID).Updates(updates).Error
	}())
}

func (b *backendStore) ApplyDialerOverride(kind string, options, override json.RawMessage) (json.RawMessage, error) {
	return coreruntime.ApplyDialerOverride(kind, options, override)
}

func (b *backendStore) LoadSubscriptionConnection(subscriptionID, connectionID uint, out *RemoteOutboundConnection) error {
	return sourceBackendError(func() error {
		return b.db.Where("subscription_id = ? AND id = ?", subscriptionID, connectionID).First(out).Error
	}())
}

func (b *backendStore) FindSourceConnection(subscriptionID uint, sourceKey string, out *RemoteOutboundConnection) error {
	return sourceBackendError(func() error {
		return b.db.Where("subscription_id = ? AND source_key = ?", subscriptionID, sourceKey).First(out).Error
	}())
}

func (b *backendStore) NextConnectionOrder() (int, error) {
	return entityorder.Next(b.db, &RemoteOutboundConnection{})
}

func (b *backendStore) DeleteConnection(connectionID uint) error {
	return sourceBackendError(func() error { return b.db.Delete(&RemoteOutboundConnection{}, connectionID).Error }())
}

func (b *backendStore) ListDueSubscriptions(now int64, out *[]RemoteOutboundSubscription) error {
	return sourceBackendError(func() error {
		return b.db.Where("enabled = ? AND auto_update = ? AND update_interval > 0 AND source_type IN ?", true, true, []string{"", SourceRemote, SourceFile}).Where("last_updated = 0 OR last_updated + update_interval <= ?", now).Order(entityorder.Clause).Find(out).Error
	}())
}

func (b *backendStore) ListRefreshIdentityConnections(subscriptionID uint, out *[]RemoteOutboundConnection) error {
	return sourceBackendError(func() error { return b.db.Where("subscription_id = ?", subscriptionID).Order("id").Find(out).Error }())
}

func (b *backendStore) DeleteGroupMemberships(groupID uint) error {
	return sourceBackendError(func() error {
		return b.db.Where("group_id = ?", groupID).Delete(&RemoteOutboundGroupConnection{}).Error
	}())
}

func (b *backendStore) DeleteGroup(groupID uint) error {
	return sourceBackendError(func() error { return b.db.Delete(&RemoteOutboundGroup{}, groupID).Error }())
}

func (b *backendStore) CountSelectedConnections(subscriptionID uint, connectionIDs []uint, out *int64) error {
	return sourceBackendError(func() error {
		return b.db.Model(&RemoteOutboundConnection{}).Where("subscription_id = ? AND id IN ?", subscriptionID, connectionIDs).Count(out).Error
	}())
}

func (b *backendStore) DeleteSelectedMemberships(groupID uint, connectionIDs []uint) error {
	return sourceBackendError(func() error {
		return b.db.Where("group_id = ? AND connection_id IN ?", groupID, connectionIDs).Delete(&RemoteOutboundGroupConnection{}).Error
	}())
}

func (b *backendStore) ListOrderedSubscriptionConnections(subscriptionID uint, out *[]RemoteOutboundConnection) error {
	return sourceBackendError(func() error {
		return b.db.Where("subscription_id = ?", subscriptionID).Order(entityorder.Clause).Find(out).Error
	}())
}

func (b *backendStore) ListOtherSyncedConnections(subscriptionID, exceptID uint, out *[]RemoteOutboundConnection) error {
	return sourceBackendError(func() error {
		return b.db.Where("subscription_id = ? AND synced = ? AND id <> ?", subscriptionID, true, exceptID).Order(entityorder.Clause).Find(out).Error
	}())
}

func (b *backendStore) ListClientGroupConnections(groupID uint, out *[]RemoteOutboundConnection) error {
	return sourceBackendError(func() error {
		return b.db.Model(&RemoteOutboundConnection{}).Joins("JOIN remote_outbound_group_connections ON remote_outbound_group_connections.connection_id = remote_outbound_connections.id").Joins("JOIN remote_outbound_groups ON remote_outbound_groups.id = remote_outbound_group_connections.group_id").Joins("JOIN remote_outbound_subscriptions ON remote_outbound_subscriptions.id = remote_outbound_connections.subscription_id").Where("remote_outbound_group_connections.group_id = ?", groupID).Where("remote_outbound_groups.enabled = ? AND remote_outbound_subscriptions.enabled = ?", true, true).Where("remote_outbound_connections.enabled = ?", true).Order("remote_outbound_groups.sort_order ASC, remote_outbound_groups.id ASC, remote_outbound_connections.sort_order ASC, remote_outbound_connections.id ASC").Find(out).Error
	}())
}

func (b *backendStore) ListClientSubscriptionConnections(subscriptionID uint, out *[]RemoteOutboundConnection) error {
	return sourceBackendError(func() error {
		return b.db.Model(&RemoteOutboundConnection{}).Joins("JOIN remote_outbound_subscriptions ON remote_outbound_subscriptions.id = remote_outbound_connections.subscription_id").Where("remote_outbound_connections.subscription_id = ?", subscriptionID).Where("remote_outbound_subscriptions.enabled = ?", true).Where("remote_outbound_connections.enabled = ?", true).Order("remote_outbound_connections.sort_order ASC, remote_outbound_connections.id ASC").Find(out).Error
	}())
}

func (b *backendStore) ListSelectedSubscriptions(subscriptionIDs []uint, out *[]RemoteOutboundSubscription) error {
	return sourceBackendError(func() error {
		return b.db.Where("id IN ?", subscriptionIDs).Order("sort_order ASC, id ASC").Find(out).Error
	}())
}

func (b *backendStore) HasSubscriptions() bool {
	return b.db.Migrator().HasTable(&RemoteOutboundSubscription{})
}

func (b *backendStore) CountDownloadReferences(tag string, out *int64) error {
	return sourceBackendError(func() error {
		return b.db.Model(&RemoteOutboundSubscription{}).Where("download_outbound_id IN (SELECT id FROM outbounds WHERE tag = ?)", tag).Count(out).Error
	}())
}

func (b *backendStore) HasConnections() bool {
	return b.db.Migrator().HasTable(&RemoteOutboundConnection{})
}

func (b *backendStore) HasGroupsAndMemberships() bool {
	return b.db.Migrator().HasTable(&RemoteOutboundGroup{}) && b.db.Migrator().HasTable(&RemoteOutboundGroupConnection{})
}

func (b *backendStore) ListTagConnections(tag string, out *[]RemoteOutboundConnection) error {
	return sourceBackendError(func() error { return b.db.Where("outbound_tag = ?", tag).Find(out).Error }())
}

func (b *backendStore) DisableConnectionGroups(connectionID uint) error {
	return sourceBackendError(func() error {
		return b.db.Model(&RemoteOutboundGroup{}).Where("outbound_enabled = ? AND id IN (SELECT group_id FROM remote_outbound_group_connections WHERE connection_id = ?)", true, connectionID).Update("outbound_enabled", false).Error
	}())
}

func (b *backendStore) UnsyncTagConnections(tag string) error {
	return sourceBackendError(func() error {
		return b.db.Model(&RemoteOutboundConnection{}).Where("outbound_tag = ?", tag).Updates(map[string]any{"synced": false, "outbound_id": nil}).Error
	}())
}

func (b *backendStore) LoadSubscription(subscriptionID uint, out *RemoteOutboundSubscription) error {
	return sourceBackendError(func() error { return b.db.First(out, subscriptionID).Error }())
}

func (b *backendStore) ListSyncedSubscriptionConnections(subscriptionID uint, out *[]RemoteOutboundConnection) error {
	return sourceBackendError(func() error {
		return b.db.Where("subscription_id = ? AND synced = ?", subscriptionID, true).Find(out).Error
	}())
}

func (b *backendStore) ListSyncedConnections(out *[]RemoteOutboundConnection) error {
	return sourceBackendError(func() error { return b.db.Where("synced = ?", true).Find(out).Error }())
}

func (b *backendStore) LoadOutbound(id uint, out *feature.OutboundRecord) error {
	return sourceBackendError(func() error {
		var row model.Outbound
		if err := b.db.First(&row, id).Error; err != nil {
			return err
		}
		*out = feature.OutboundRecord{ID: row.Id, Tag: row.Tag, Type: row.Type, Options: row.Options}
		return nil
	}())
}

func (b *backendStore) MarkConnectionUnsynced(connectionID uint, now int64) error {
	return sourceBackendError(func() error {
		return b.db.Model(&RemoteOutboundConnection{}).Where("id = ?", connectionID).Updates(map[string]any{"synced": false, "outbound_id": nil, "updated_at": now}).Error
	}())
}

func (b *backendStore) BindingOwner(connection RemoteOutboundConnection) feature.BindingOwner {
	return backendoutbounds.New(b.db, backendoutbounds.Source{Id: connection.Id, SubscriptionId: connection.SubscriptionId, SourceKey: connection.SourceKey, Name: connection.Name, Type: connection.Type, OutboundTag: connection.OutboundTag, OutboundId: connection.OutboundId, Enabled: connection.Enabled, Options: connection.Options, Canonical: connection.Canonical})
}

func (b *backendStore) RetagConnectionOutbound(connection RemoteOutboundConnection, oldTag, newTag string) (bool, error) {
	return backendoutbounds.New(b.db, backendoutbounds.Source{Id: connection.Id, SubscriptionId: connection.SubscriptionId, SourceKey: connection.SourceKey, Name: connection.Name, Type: connection.Type, OutboundTag: connection.OutboundTag, OutboundId: connection.OutboundId, Enabled: connection.Enabled, Options: connection.Options, Canonical: connection.Canonical}).Retag(b.Context(), oldTag, newTag)
}

func (b *backendStore) CountOutboundTag(tag string, out *int64) error {
	return sourceBackendError(func() error { return b.db.Model(&model.Outbound{}).Where("tag = ?", tag).Count(out).Error }())
}

func (b *backendStore) CountOwnConnectionTag(connectionID uint, tag string, out *int64) error {
	return sourceBackendError(func() error {
		return b.db.Model(&RemoteOutboundConnection{}).Where("outbound_tag = ? AND id = ?", tag, connectionID).Count(out).Error
	}())
}

func (b *backendStore) CountOtherConnectionTags(connectionID uint, tag string, out *int64) error {
	return sourceBackendError(func() error {
		query := b.db.Model(&RemoteOutboundConnection{}).Where("outbound_tag = ?", tag)
		if connectionID != 0 {
			query = query.Where("id <> ?", connectionID)
		}
		return query.Count(out).Error
	}())
}

func (b *backendStore) FindMissingOutbound(tag string, out *feature.OutboundRecord) error {
	return sourceBackendError(func() error {
		var row model.Outbound
		if err := b.db.Where("tag = ? AND remote_missing = ?", tag, true).First(&row).Error; err != nil {
			return err
		}
		*out = feature.OutboundRecord{ID: row.Id, Tag: row.Tag, Type: row.Type, Options: row.Options}
		return nil
	}())
}

func (b *backendStore) CountOutboundConnections(outboundID uint, tag string, out *int64) error {
	return sourceBackendError(func() error {
		return b.db.Model(&RemoteOutboundConnection{}).Where("outbound_id = ? OR outbound_tag = ?", outboundID, tag).Count(out).Error
	}())
}

func (b *backendStore) ListEnabledConnections(out *[]RemoteOutboundConnection) error {
	return b.db.Where("enabled = ?", true).Order("subscription_id ASC, sort_order ASC, id ASC").Find(out).Error
}
func (b *backendStore) ProbeConnection(ctx context.Context, c RemoteOutboundConnection, target string) CheckOutboundResult {
	return CheckOutboundResult(CheckConnectionWithTempCoreDB(ctx, b.db, c, target))
}
func (b *backendStore) ClassifyCheckError(err error) string {
	return coreruntime.ClassifyOutboundCheckError(err)
}
