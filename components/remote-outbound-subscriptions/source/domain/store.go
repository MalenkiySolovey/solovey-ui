//go:build !minimal

package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	hostoutbounds "github.com/MalenkiySolovey/solovey-host/outbounds"
	"strings"
)

var ErrRecordNotFound = errors.New("source record not found")

func sourceError(values ...any) error {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprint(v)
	}
	return errors.New(strings.Join(parts, " "))
}

type RefreshSnapshot struct {
	TagPrefix             string
	UpdatedAt             int64
	Canonical, Collection json.RawMessage
}
type OutboundRecord struct {
	ID        uint
	Tag, Type string
	Options   json.RawMessage
}
type BindingOwner interface {
	hostoutbounds.Owner
	hostoutbounds.Reader
	BindingRef() hostoutbounds.BindingRef
	Prepare(context.Context, hostoutbounds.Action, json.RawMessage, hostoutbounds.Missing) (hostoutbounds.Candidate, error)
}

// Store is the finite Backend view for the four source tables and admitted
// Outbounds operations. Its implementation owns the current transaction.
type Store interface {
	ListEnabledConnections(*[]RemoteOutboundConnection) error
	ProbeConnection(context.Context, RemoteOutboundConnection, string) CheckOutboundResult
	ClassifyCheckError(error) string
	Context() context.Context
	ListGroupConnections(groupID uint, out *[]RemoteOutboundConnection) error
	ListGroupMemberships(groupID uint, out *[]RemoteOutboundGroupConnection) error
	CountMembership(groupID, connectionID uint, out *int64) error
	CreateMembership(link *RemoteOutboundGroupConnection) error
	CountConnectionMemberships(connectionID uint, out *int64) error
	ListSubscriptionIDs(out *[]RemoteOutboundSubscription) error
	ListSubscriptionConnections(subscriptionID uint, out *[]RemoteOutboundConnection) error
	CountSubscriptionGroup(subscriptionID, groupID uint, out *int64) error
	ListMembershipsForConnections(connectionIDs []uint, out *[]RemoteOutboundGroupConnection) error
	ListSubscriptionConnectionIDs(subscriptionID uint, out *[]RemoteOutboundConnection) error
	FirstConnectionGroup(connectionID uint, out *RemoteOutboundGroup) error
	SetLegacyGroup(connectionID, groupID uint) error
	CountOutboundGroupsForConnection(connectionID uint, out *int64) error
	ListOutboundEnabledGroups(out *[]RemoteOutboundGroup) error
	DisableGroupOutbounds(groupID uint, now int64) error
	FindDefaultGroup(subscriptionID uint, out *RemoteOutboundGroup) error
	NextGroupOrder() (int, error)
	CreateGroup(group *RemoteOutboundGroup) error
	LoadConnection(connectionID uint, out *RemoteOutboundConnection) error
	LoadGroup(groupID uint, out *RemoteOutboundGroup) error
	SaveConnection(connection *RemoteOutboundConnection) error
	SaveGroup(group *RemoteOutboundGroup) error
	DeleteConnectionMemberships(connectionID uint) error
	MarkRefreshError(subscriptionID uint, message string, now int64) error
	CommitRefresh(subscriptionID uint, snapshot RefreshSnapshot) error
	ApplyDialerOverride(kind string, options, override json.RawMessage) (json.RawMessage, error)
	LoadSubscriptionConnection(subscriptionID, connectionID uint, out *RemoteOutboundConnection) error
	FindSourceConnection(subscriptionID uint, sourceKey string, out *RemoteOutboundConnection) error
	NextConnectionOrder() (int, error)
	DeleteConnection(connectionID uint) error
	ListDueSubscriptions(now int64, out *[]RemoteOutboundSubscription) error
	ListRefreshIdentityConnections(subscriptionID uint, out *[]RemoteOutboundConnection) error
	DeleteGroupMemberships(groupID uint) error
	DeleteGroup(groupID uint) error
	CountSelectedConnections(subscriptionID uint, connectionIDs []uint, out *int64) error
	DeleteSelectedMemberships(groupID uint, connectionIDs []uint) error
	ListOrderedSubscriptionConnections(subscriptionID uint, out *[]RemoteOutboundConnection) error
	ListOtherSyncedConnections(subscriptionID, exceptID uint, out *[]RemoteOutboundConnection) error
	ListClientGroupConnections(groupID uint, out *[]RemoteOutboundConnection) error
	ListClientSubscriptionConnections(subscriptionID uint, out *[]RemoteOutboundConnection) error
	ListSelectedSubscriptions(subscriptionIDs []uint, out *[]RemoteOutboundSubscription) error
	HasSubscriptions() bool
	CountDownloadReferences(tag string, out *int64) error
	HasConnections() bool
	HasGroupsAndMemberships() bool
	ListTagConnections(tag string, out *[]RemoteOutboundConnection) error
	DisableConnectionGroups(connectionID uint) error
	UnsyncTagConnections(tag string) error
	LoadSubscription(subscriptionID uint, out *RemoteOutboundSubscription) error
	ListSyncedSubscriptionConnections(subscriptionID uint, out *[]RemoteOutboundConnection) error
	ListSyncedConnections(out *[]RemoteOutboundConnection) error
	LoadOutbound(id uint, out *OutboundRecord) error
	MarkConnectionUnsynced(connectionID uint, now int64) error
	BindingOwner(connection RemoteOutboundConnection) BindingOwner
	RetagConnectionOutbound(connection RemoteOutboundConnection, oldTag, newTag string) (bool, error)
	CountOutboundTag(tag string, out *int64) error
	CountOwnConnectionTag(connectionID uint, tag string, out *int64) error
	CountOtherConnectionTags(connectionID uint, tag string, out *int64) error
	FindMissingOutbound(tag string, out *OutboundRecord) error
	CountOutboundConnections(outboundID uint, tag string, out *int64) error
}
