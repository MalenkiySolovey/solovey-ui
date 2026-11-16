//go:build !minimal

package remote

import (
	"encoding/json"
	"errors"

	subcanonical "github.com/MalenkiySolovey/solovey-host/subscriptions/canonical"
)

type RefreshResult struct {
	SubscriptionId uint `json:"subscriptionId"`
	Fetched        int  `json:"fetched"`
	Created        int  `json:"created"`
	Updated        int  `json:"updated"`
	MarkedMissing  int  `json:"markedMissing"`
	Synced         int  `json:"synced"`
	PruneDeferred  bool `json:"pruneDeferred,omitempty"`
}

func MarkRefreshError(tx Store, subscriptionID uint, message string, now int64) error {
	return tx.MarkRefreshError(subscriptionID, message, now)
}

func RefreshFetchedSubscription(tx Store, subscription *RemoteOutboundSubscription, fetched *FetchedSubscription, now int64) (RefreshResult, bool, error) {
	if fetched == nil {
		return RefreshResult{SubscriptionId: subscription.Id}, false, nil
	}
	collectionSnapshot, err := refreshCollectionSnapshot(fetched)
	if err != nil {
		return RefreshResult{SubscriptionId: subscription.Id}, false, err
	}
	return refreshSubscriptionOutbounds(tx, subscription, fetched.Outbounds, fetched.Snapshot, collectionSnapshot, fetched.Complete(), now)
}

func RefreshSubscriptionOutbounds(tx Store, subscription *RemoteOutboundSubscription, outbounds []map[string]interface{}, now int64) (RefreshResult, bool, error) {
	canonicalSnapshot := subcanonical.ObserveOutbounds(subcanonical.FormatSingBox, outbounds)
	return refreshSubscriptionOutbounds(tx, subscription, outbounds, canonicalSnapshot, nil, true, now)
}

func refreshSubscriptionOutbounds(tx Store, subscription *RemoteOutboundSubscription, outbounds []map[string]interface{}, canonicalSnapshot subcanonical.Snapshot, collectionSnapshot json.RawMessage, allowPrune bool, now int64) (RefreshResult, bool, error) {
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
	missingChanged := false
	if allowPrune {
		missingChanged, err = pruneStaleConnections(tx, subscription.Id, seen, now, &result)
		if err != nil {
			return result, false, err
		}
	} else {
		result.PruneDeferred = true
	}
	canonicalSnapshotData, err := json.Marshal(canonicalSnapshot)
	if err != nil {
		return result, false, err
	}
	updates := RefreshSnapshot{TagPrefix: subscription.TagPrefix, UpdatedAt: now, Canonical: canonicalSnapshotData, Collection: collectionSnapshot}
	if err := tx.CommitRefresh(subscription.Id, updates); err != nil {
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

func refreshSeenConnections(tx Store, subscription *RemoteOutboundSubscription, defaultGroupID uint, outbounds []map[string]interface{}, canonicalSnapshot subcanonical.Snapshot, now int64, result *RefreshResult) (map[string]struct{}, bool, error) {
	identities, err := NewEndpointRefreshPlan(tx, subscription.Id, outbounds, canonicalSnapshot)
	if err != nil {
		return nil, false, err
	}
	seen := make(map[string]struct{}, len(outbounds))
	syncedChanged := false
	for index, outbound := range outbounds {
		connection, wasSynced, err := upsertRefreshConnection(tx, subscription, defaultGroupID, outbound, CanonicalConnectionAt(canonicalSnapshot, index), index, seen, identities, now, result)
		if err != nil {
			return nil, false, err
		}
		shouldSync, err := ConnectionUsesOutboundEnabledGroup(tx, connection.Id)
		if err != nil {
			return nil, false, err
		}
		if (connection.Synced || shouldSync) && connection.Enabled {
			if err := SyncConnectionToOutbound(tx, &connection, false); err != nil {
				return nil, false, err
			}
			if err := tx.SaveConnection(&connection); err != nil {
				return nil, false, err
			}
			result.Synced++
			syncedChanged = syncedChanged || !wasSynced
		}
	}
	return seen, syncedChanged, nil
}

func upsertRefreshConnection(tx Store, subscription *RemoteOutboundSubscription, defaultGroupID uint, outbound map[string]interface{}, CanonicalConnection *subcanonical.Connection, index int, seen map[string]struct{}, identities *EndpointRefreshPlan, now int64, result *RefreshResult) (RemoteOutboundConnection, bool, error) {
	normalized, err := NormalizeOutboundWithCanonical(outbound, index, CanonicalConnection)
	if err != nil {
		return RemoteOutboundConnection{}, false, err
	}
	normalized.SourceKey = UniqueSourceKey(seen, normalized.SourceKey)
	normalized.Name = SourceDisplayName(normalized.Name, index, subscription.RemoveEmojis)
	if !ConnectionIsGroup(RemoteOutboundConnection{Type: normalized.Type, Options: normalized.Options, Canonical: normalized.Canonical}) {
		normalized.Options, err = tx.ApplyDialerOverride(normalized.Type, normalized.Options, subscription.DialerOverride)
		if err != nil {
			return RemoteOutboundConnection{}, false, err
		}
	}

	var connection RemoteOutboundConnection
	var previous *RemoteOutboundConnection
	if planned, ok := identities.matches[index]; ok {
		locator := normalized.SourceKey
		normalized.SourceKey = planned.SourceKey
		// Retain the already-supported typed-label migration when it is
		// unambiguous; ordinary renames keep the existing opaque locator.
		if planned.SourceKey == LegacyTypedLabelSourceKey(normalized.Type, locator) {
			if _, occupied := identities.occupied[locator]; !occupied {
				normalized.SourceKey = locator
			}
		}
		err = tx.LoadSubscriptionConnection(subscription.Id, planned.Id, &connection)
	} else {
		err = tx.FindSourceConnection(subscription.Id, normalized.SourceKey, &connection)
		if err == nil && identities.unavailable(connection.Id, index) {
			err = ErrRecordNotFound
		}
	}
	switch {
	case err == nil:
		if connection.SourceKey != normalized.SourceKey {
			connection.SourceKey = normalized.SourceKey
			result.Updated++
		}
		before := connection
		if UpdateConnection(&connection, normalized, now) {
			previous = &before
			result.Updated++
		}
	case errors.Is(err, ErrRecordNotFound):
		legacy, err := findConnectionByLegacySourceKey(tx, subscription.Id, normalized)
		if err != nil {
			return RemoteOutboundConnection{}, false, err
		}
		if legacy != nil && identities.unavailable(legacy.Id, index) {
			legacy = nil
		}
		if legacy != nil {
			connection = *legacy
			if connection.SourceKey != normalized.SourceKey {
				connection.SourceKey = normalized.SourceKey
				result.Updated++
			}
			before := connection
			if UpdateConnection(&connection, normalized, now) {
				previous = &before
				result.Updated++
			}
		} else {
			normalized.SourceKey = UniqueSourceKey(identities.occupied, normalized.SourceKey)
			tag, err := UniqueOutboundTag(tx, *subscription, normalized.Name, 0)
			if err != nil {
				return RemoteOutboundConnection{}, false, err
			}
			sortOrder := normalized.SortOrder
			if sortOrder <= 0 {
				nextSortOrder, err := tx.NextConnectionOrder()
				if err != nil {
					return RemoteOutboundConnection{}, false, err
				}
				sortOrder = nextSortOrder
			}
			connection = RemoteOutboundConnection{
				SubscriptionId: subscription.Id,
				GroupId:        defaultGroupID,
				SortOrder:      sortOrder,
				Name:           normalized.Name,
				SourceKey:      normalized.SourceKey,
				Type:           normalized.Type,
				OutboundTag:    tag,
				Enabled:        true,
				Synced:         false,
				Options:        normalized.Options,
				Canonical:      normalized.Canonical,
				LastSeen:       now,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			if outboundID, err := ReusableRemoteMissingOutboundID(tx, tag); err != nil {
				return RemoteOutboundConnection{}, false, err
			} else if outboundID != 0 {
				connection.OutboundId = &outboundID
			}
			result.Created++
		}
	default:
		return RemoteOutboundConnection{}, false, err
	}

	wasSynced := connection.Synced
	if wasSynced && previous != nil {
		// Compare the same checked projection the materializer consumes.
		// Source labels/provenance alone must not restart the runtime.
		before, err := ConnectionOutboundConfigWithTags(*previous, nil)
		if err != nil {
			return RemoteOutboundConnection{}, false, err
		}
		after, err := ConnectionOutboundConfigWithTags(connection, nil)
		if err != nil {
			return RemoteOutboundConnection{}, false, err
		}
		wasSynced = JSONRawEqual(before, after)
	}
	if err := tx.SaveConnection(&connection); err != nil {
		return RemoteOutboundConnection{}, false, err
	}
	identities.claimed[connection.Id] = true
	identities.occupied[connection.SourceKey] = struct{}{}
	seen[connection.SourceKey] = struct{}{}
	if connection.GroupId != 0 {
		if err := EnsureConnectionHasGroup(tx, connection.Id, defaultGroupID); err != nil {
			return RemoteOutboundConnection{}, false, err
		}
	}
	if err := SyncConnectionLegacyGroupID(tx, connection.Id); err != nil {
		return RemoteOutboundConnection{}, false, err
	}
	return connection, wasSynced, nil
}

func findConnectionByLegacySourceKey(tx Store, subscriptionID uint, normalized NormalizedOutbound) (*RemoteOutboundConnection, error) {
	legacyKey := LegacyTypedLabelSourceKey(normalized.Type, normalized.SourceKey)
	if legacyKey == "" || legacyKey == normalized.SourceKey {
		return nil, nil
	}
	var connection RemoteOutboundConnection
	err := tx.FindSourceConnection(subscriptionID, legacyKey, &connection)
	if errors.Is(err, ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &connection, nil
}

func CanonicalConnectionAt(snapshot subcanonical.Snapshot, index int) *subcanonical.Connection {
	if index < 0 || index >= len(snapshot.Connections) {
		return nil
	}
	return &snapshot.Connections[index]
}

func pruneStaleConnections(tx Store, subscriptionID uint, seen map[string]struct{}, now int64, result *RefreshResult) (bool, error) {
	var existing []RemoteOutboundConnection
	if err := tx.ListSubscriptionConnections(subscriptionID, &existing); err != nil {
		return false, err
	}
	syncedChanged := false
	for _, connection := range existing {
		if _, ok := seen[connection.SourceKey]; ok {
			continue
		}
		if connection.Synced {
			if err := MarkConnectionOutboundMissing(tx, &connection, "not present in latest successful refresh", now); err != nil {
				return false, err
			}
			syncedChanged = true
		}
		if err := tx.DeleteConnectionMemberships(connection.Id); err != nil {
			return false, err
		}
		if err := tx.DeleteConnection(connection.Id); err != nil {
			return false, err
		}
		result.MarkedMissing++
	}
	return syncedChanged, nil
}

func DueSubscriptions(tx Store, now int64) ([]RemoteOutboundSubscription, error) {
	var subscriptions []RemoteOutboundSubscription
	err := tx.ListDueSubscriptions(now, &subscriptions)
	return subscriptions, err
}
