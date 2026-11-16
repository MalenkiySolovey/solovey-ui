//go:build !minimal

package remote

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-host/hostref"
	hostoutbounds "github.com/MalenkiySolovey/solovey-host/outbounds"
)

func SyncConnectionToOutbound(tx Store, connection *RemoteOutboundConnection, requireUsable bool) error {
	return syncConnectionToOutbound(tx, connection, requireUsable, map[uint]struct{}{})
}

func UnsyncConnectionsForDeletedOutbound(tx Store, tag string) error {
	if err := validateDownloadOutboundDeletion(tx, tag); err != nil {
		return err
	}
	return unsyncConnectionsForDeletedOutbound(tx, tag)
}

func validateDownloadOutboundDeletion(tx Store, tag string) error {
	if tx.HasSubscriptions() {
		var references int64
		if err := tx.CountDownloadReferences(tag, &references); err != nil {
			return err
		}
		if references != 0 {
			return sourceError("SUBSCRIPTION_DOWNLOAD_OUTBOUND_REFERENCED")
		}
	}
	return nil
}

func unsyncConnectionsForDeletedOutbound(tx Store, tag string) error {
	if !tx.HasConnections() {
		return nil
	}
	var linkedConnections []RemoteOutboundConnection
	if err := tx.ListTagConnections(tag, &linkedConnections); err != nil {
		return err
	}
	if tx.HasGroupsAndMemberships() {
		for _, connection := range linkedConnections {
			if err := tx.DisableConnectionGroups(connection.Id); err != nil {
				return err
			}
		}
	}
	return tx.UnsyncTagConnections(tag)
}

func syncConnectionToOutbound(tx Store, connection *RemoteOutboundConnection, requireUsable bool, visited map[uint]struct{}) error {
	if connection == nil {
		return sourceError("connection is required")
	}
	if connection.Id != 0 {
		if _, ok := visited[connection.Id]; ok {
			return nil
		}
		visited[connection.Id] = struct{}{}
	}
	if ConnectionIsGroup(*connection) {
		if err := syncGroupDependencyOutbounds(tx, *connection, requireUsable, visited); err != nil {
			return err
		}
	}
	if err := syncSingleConnectionToOutbound(tx, connection, requireUsable); err != nil {
		return err
	}
	return nil
}
func syncSingleConnectionToOutbound(tx Store, connection *RemoteOutboundConnection, requireUsable bool) error {
	if requireUsable {
		switch {
		case !connection.Enabled:
			return sourceError("disabled connection can not be synced")
		}
	}
	if strings.TrimSpace(connection.OutboundTag) == "" {
		return sourceError("connection outbound tag is empty")
	}
	config, err := ConnectionOutboundConfigForSync(tx, *connection)
	if err != nil {
		return err
	}
	owner := tx.BindingOwner(*connection)
	candidate, err := owner.Prepare(tx.Context(), hostoutbounds.Upsert, config, hostoutbounds.Missing{})
	if err != nil {
		return err
	}
	receipt, err := hostoutbounds.ApplyBinding(tx.Context(), candidate, owner)
	if err != nil {
		return err
	}
	if receipt.State != hostoutbounds.Pending && receipt.State != hostoutbounds.Applied {
		return hostoutbounds.ErrObservation
	}
	connection.OutboundId = &receipt.Resource.ID
	connection.Synced = true
	connection.UpdatedAt = time.Now().Unix()
	return nil
}
func MarkConnectionOutboundMissing(tx Store, connection *RemoteOutboundConnection, reason string, now int64) error {
	if connection == nil {
		return nil
	}
	if strings.TrimSpace(reason) == "" {
		reason = "not present in latest successful refresh"
	}
	owner := tx.BindingOwner(*connection)
	candidate, err := owner.Prepare(tx.Context(), hostoutbounds.MarkMissing, nil, hostoutbounds.Missing{Reason: reason, Since: now, Source: remoteMissingSource(tx, *connection)})
	if err != nil {
		return err
	}
	if candidate.ExpectedResourceRevision == "ABSENT" {
		return nil
	}
	_, err = hostoutbounds.ApplyBinding(tx.Context(), candidate, owner)
	return err
}

func remoteMissingSource(tx Store, connection RemoteOutboundConnection) string {
	var subscription RemoteOutboundSubscription
	if connection.SubscriptionId != 0 && tx.LoadSubscription(connection.SubscriptionId, &subscription) == nil {
		if strings.TrimSpace(connection.Name) != "" {
			return strings.TrimSpace(subscription.Name + " / " + connection.Name)
		}
		return strings.TrimSpace(subscription.Name)
	}
	return strings.TrimSpace(connection.Name)
}
func ConnectionOutboundConfigForSync(tx Store, connection RemoteOutboundConnection) (json.RawMessage, error) {
	tagMap, err := subscriptionConnectionTagMap(tx, connection.SubscriptionId)
	if err != nil {
		return nil, err
	}
	return ConnectionOutboundConfigWithTags(connection, tagMap)
}
func subscriptionConnectionTagMap(tx Store, subscriptionID uint) (map[string]string, error) {
	if subscriptionID == 0 {
		return nil, nil
	}
	var connections []RemoteOutboundConnection
	if err := tx.ListSubscriptionConnections(subscriptionID, &connections); err != nil {
		return nil, err
	}
	return ConnectionTagMap(connections), nil
}
func ConnectionTagMap(connections []RemoteOutboundConnection) map[string]string {
	tagMap := make(map[string]string, len(connections)*2)
	for _, connection := range connections {
		outboundTag := strings.TrimSpace(connection.OutboundTag)
		if outboundTag == "" {
			continue
		}
		tagMap[outboundTag] = outboundTag
		name := connection.Name
		if captured := CanonicalConnection(connection.Canonical); captured != nil && captured.DisplayName != "" {
			name = captured.DisplayName
		}
		if name = strings.TrimSpace(name); name != "" {
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
func UnsyncConnectionFromOutbound(tx Store, connection *RemoteOutboundConnection) error {
	now := time.Now().Unix()
	if _, err := deleteLinkedOutbound(tx, *connection); err != nil {
		return err
	}
	connection.Synced = false
	connection.OutboundId = nil
	connection.UpdatedAt = now
	return tx.SaveConnection(connection)
}
func DeleteSyncedOutboundsForSubscription(tx Store, subscriptionID uint) (bool, error) {
	var connections []RemoteOutboundConnection
	if err := tx.ListSyncedSubscriptionConnections(subscriptionID, &connections); err != nil {
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
func deleteLinkedOutbound(tx Store, connection RemoteOutboundConnection) (bool, error) {
	if connection.OutboundId == nil || *connection.OutboundId == 0 {
		return false, nil
	}
	owner := tx.BindingOwner(connection)
	candidate, err := owner.Prepare(tx.Context(), hostoutbounds.Remove, nil, hostoutbounds.Missing{})
	if err != nil {
		return false, err
	}
	if candidate.ExpectedResourceRevision == "ABSENT" {
		return false, nil
	}
	_, err = hostoutbounds.ApplyBinding(tx.Context(), candidate, owner)
	return err == nil, err
}

func ReconcileOutboundLinks(tx Store) (bool, error) {
	if !tx.HasConnections() {
		return false, nil
	}
	var connections []RemoteOutboundConnection
	if err := tx.ListSyncedConnections(&connections); err != nil {
		return false, err
	}
	changed := false
	now := time.Now().Unix()
	for _, connection := range connections {
		if connection.OutboundId == nil || *connection.OutboundId == 0 {
			if err := MarkConnectionUnsynced(tx, connection.Id, now); err != nil {
				return false, err
			}
			changed = true
			continue
		}
		query := hostoutbounds.Query{Binding: tx.BindingOwner(connection).BindingRef(), Resource: hostoutbounds.ResourceRef{Host: hostref.Local(), ID: *connection.OutboundId, Tag: connection.OutboundTag}}
		observed, observeErr := hostoutbounds.ObserveBinding(tx.Context(), query, tx.BindingOwner(connection))
		if observeErr != nil {
			return false, observeErr
		}
		if !observed.Present {
			if err := MarkConnectionUnsynced(tx, connection.Id, now); err != nil {
				return false, err
			}
			changed = true
			continue
		}
		var outbound OutboundRecord
		err := tx.LoadOutbound(*connection.OutboundId, &outbound)
		if err == nil && outbound.Tag == connection.OutboundTag {
			matches, matchErr := OutboundMatchesRemoteConnection(tx, connection, outbound)
			if matchErr != nil {
				return false, matchErr
			}
			if matches {
				continue
			}
		}
		if err != nil && !errors.Is(err, ErrRecordNotFound) {
			return false, err
		}
		if err := MarkConnectionUnsynced(tx, connection.Id, now); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}
func OutboundMatchesRemoteConnection(tx Store, connection RemoteOutboundConnection, outbound OutboundRecord) (bool, error) {
	config, err := ConnectionOutboundConfigForSync(tx, connection)
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
func MarkConnectionUnsynced(tx Store, connectionID uint, now int64) error {
	return tx.MarkConnectionUnsynced(connectionID, now)
}
