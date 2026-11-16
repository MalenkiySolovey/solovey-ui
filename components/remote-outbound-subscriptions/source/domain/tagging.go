//go:build !minimal

package remote

import (
	"errors"
	"fmt"
	"time"
)

func RetagSubscriptionConnections(tx Store, subscription *RemoteOutboundSubscription) (bool, error) {
	if subscription == nil || subscription.Id == 0 {
		return false, nil
	}
	var connections []RemoteOutboundConnection
	if err := tx.ListOrderedSubscriptionConnections(subscription.Id, &connections); err != nil {
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
			changed, err := tx.RetagConnectionOutbound(*connection, oldTag, newTag)
			if err != nil {
				return false, err
			}
			if changed {
				coreRestart = true
			} else {
				connection.Synced = false
				connection.OutboundId = nil
			}
		}
		connection.OutboundTag = newTag
		connection.UpdatedAt = now
		if err := tx.SaveConnection(connection); err != nil {
			return false, err
		}
	}
	return coreRestart, nil
}
func UniqueOutboundTag(tx Store, subscription RemoteOutboundSubscription, name string, connectionID uint) (string, error) {
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
	return "", sourceError("unable to allocate unique outbound tag")
}
func OutboundTagAvailable(tx Store, tag string, connectionID uint) (bool, error) {
	var outbounds int64
	if err := tx.CountOutboundTag(tag, &outbounds); err != nil {
		return false, err
	}
	if outbounds > 0 {
		if connectionID == 0 {
			return reusableRemoteMissingOutboundExists(tx, tag)
		}
		var linked int64
		if err := tx.CountOwnConnectionTag(connectionID, tag, &linked); err != nil {
			return false, err
		}
		return linked > 0, nil
	}
	var connections int64
	if err := tx.CountOtherConnectionTags(connectionID, tag, &connections); err != nil {
		return false, err
	}
	return connections == 0, nil
}

func reusableRemoteMissingOutboundExists(tx Store, tag string) (bool, error) {
	outboundID, err := ReusableRemoteMissingOutboundID(tx, tag)
	return outboundID != 0, err
}

func ReusableRemoteMissingOutboundID(tx Store, tag string) (uint, error) {
	var outbound OutboundRecord
	if err := tx.FindMissingOutbound(tag, &outbound); err != nil {
		if errors.Is(err, ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	var linked int64
	if err := tx.CountOutboundConnections(outbound.ID, tag, &linked); err != nil {
		return 0, err
	}
	if linked > 0 {
		return 0, nil
	}
	return outbound.ID, nil
}
