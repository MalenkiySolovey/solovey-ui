//go:build !minimal

package remote

import (
	"encoding/json"
	feature "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/source/domain"
	"gorm.io/gorm"
)

func MarkRefreshError(tx *gorm.DB, subscriptionID uint, message string, now int64) error {
	return feature.MarkRefreshError(sourceStore(tx), subscriptionID, message, now)
}
func RefreshFetchedSubscription(tx *gorm.DB, subscription *RemoteOutboundSubscription, fetched *FetchedSubscription, now int64) (RefreshResult, bool, error) {
	return feature.RefreshFetchedSubscription(sourceStore(tx), subscription, fetched, now)
}
func RefreshSubscriptionOutbounds(tx *gorm.DB, subscription *RemoteOutboundSubscription, outbounds []map[string]interface{}, now int64) (RefreshResult, bool, error) {
	return feature.RefreshSubscriptionOutbounds(sourceStore(tx), subscription, outbounds, now)
}
func DueSubscriptions(tx *gorm.DB, now int64) ([]RemoteOutboundSubscription, error) {
	return feature.DueSubscriptions(sourceStore(tx), now)
}
func GroupConnections(tx *gorm.DB, groupID uint) ([]RemoteOutboundConnection, error) {
	return feature.GroupConnections(sourceStore(tx), groupID)
}
func GroupConnectionIDSet(tx *gorm.DB, groupID uint) (map[uint]struct{}, error) {
	return feature.GroupConnectionIDSet(sourceStore(tx), groupID)
}
func AddGroupConnection(tx *gorm.DB, groupID uint, connectionID uint, now int64) error {
	return feature.AddGroupConnection(sourceStore(tx), groupID, connectionID, now)
}
func EnsureConnectionHasGroup(tx *gorm.DB, connectionID uint, fallbackGroupID uint) error {
	return feature.EnsureConnectionHasGroup(sourceStore(tx), connectionID, fallbackGroupID)
}
func BackfillGroupMemberships(tx *gorm.DB) error {
	return feature.BackfillGroupMemberships(sourceStore(tx))
}
func BackfillSubscriptionGroupMemberships(tx *gorm.DB, subscriptionID uint) error {
	return feature.BackfillSubscriptionGroupMemberships(sourceStore(tx), subscriptionID)
}
func HydrateConnectionGroupIDs(tx *gorm.DB, subscriptions []RemoteOutboundSubscription) error {
	return feature.HydrateConnectionGroupIDs(sourceStore(tx), subscriptions)
}
func SyncLegacyGroupIDs(tx *gorm.DB, subscriptionID uint) error {
	return feature.SyncLegacyGroupIDs(sourceStore(tx), subscriptionID)
}
func SyncConnectionLegacyGroupID(tx *gorm.DB, connectionID uint) error {
	return feature.SyncConnectionLegacyGroupID(sourceStore(tx), connectionID)
}
func ConnectionUsesOutboundEnabledGroup(tx *gorm.DB, connectionID uint) (bool, error) {
	return feature.ConnectionUsesOutboundEnabledGroup(sourceStore(tx), connectionID)
}
func ReconcileGroupStates(tx *gorm.DB) error { return feature.ReconcileGroupStates(sourceStore(tx)) }
func FilterUsableConnections(connections []RemoteOutboundConnection) []RemoteOutboundConnection {
	return feature.FilterUsableConnections(connections)
}
func ConnectionsAllSynced(connections []RemoteOutboundConnection) bool {
	return feature.ConnectionsAllSynced(connections)
}
func MapKeys(values map[uint]struct{}) []uint { return feature.MapKeys(values) }
func EnsureDefaultGroup(tx *gorm.DB, subscriptionID uint, now int64) (*RemoteOutboundGroup, error) {
	return feature.EnsureDefaultGroup(sourceStore(tx), subscriptionID, now)
}
func DeleteGroup(tx *gorm.DB, id uint, now int64) (RemoteOutboundGroup, error) {
	return feature.DeleteGroup(sourceStore(tx), id, now)
}
func MoveConnectionToGroup(tx *gorm.DB, connectionID uint, groupID uint, now int64) (RemoteOutboundConnection, error) {
	return feature.MoveConnectionToGroup(sourceStore(tx), connectionID, groupID, now)
}
func SetGroupConnections(tx *gorm.DB, groupID uint, connectionIDs []uint, now int64) (bool, error) {
	return feature.SetGroupConnections(sourceStore(tx), groupID, connectionIDs, now)
}
func ToggleGroupOutbounds(tx *gorm.DB, groupID uint, now int64) (*GroupActionResult, bool, error) {
	return feature.ToggleGroupOutbounds(sourceStore(tx), groupID, now)
}
func UnsyncConnectionIfNoOutboundGroup(tx *gorm.DB, connectionID uint) (bool, error) {
	return feature.UnsyncConnectionIfNoOutboundGroup(sourceStore(tx), connectionID)
}
func ReconcileDerivedGroupDependencies(tx *gorm.DB, subscriptionID uint, defaultGroupID uint) error {
	return feature.ReconcileDerivedGroupDependencies(sourceStore(tx), subscriptionID, defaultGroupID)
}
func ExpandConnectionsWithGroupDependencies(tx *gorm.DB, connections []RemoteOutboundConnection) ([]RemoteOutboundConnection, error) {
	return feature.ExpandConnectionsWithGroupDependencies(sourceStore(tx), connections)
}
func groupMemberRefs(connection RemoteOutboundConnection) []string {
	return feature.GroupMemberRefs(connection)
}
func connectionMatchesMemberRef(connection RemoteOutboundConnection, ref string) bool {
	return feature.ConnectionMatchesMemberRef(connection, ref)
}
func OutboundsForClientLinks(db *gorm.DB, rawLinks json.RawMessage) ([]map[string]interface{}, []string, error) {
	return feature.OutboundsForClientLinks(sourceStore(db), rawLinks)
}
func OutboundsForClientLinksWithOptions(db *gorm.DB, rawLinks json.RawMessage, options ClientConversionOptions) ([]map[string]interface{}, []string, error) {
	return feature.OutboundsForClientLinksWithOptions(sourceStore(db), rawLinks, options)
}
func SyncConnectionToOutbound(tx *gorm.DB, connection *RemoteOutboundConnection, requireUsable bool) error {
	return feature.SyncConnectionToOutbound(sourceStore(tx), connection, requireUsable)
}
func UnsyncConnectionsForDeletedOutbound(tx *gorm.DB, tag string) error {
	return feature.UnsyncConnectionsForDeletedOutbound(sourceStore(tx), tag)
}
func MarkConnectionOutboundMissing(tx *gorm.DB, connection *RemoteOutboundConnection, reason string, now int64) error {
	return feature.MarkConnectionOutboundMissing(sourceStore(tx), connection, reason, now)
}
func connectionOutboundConfigForSync(tx *gorm.DB, connection RemoteOutboundConnection) (json.RawMessage, error) {
	return feature.ConnectionOutboundConfigForSync(sourceStore(tx), connection)
}
func remoteConnectionTagMap(connections []RemoteOutboundConnection) map[string]string {
	return feature.ConnectionTagMap(connections)
}
func rewriteOutboundTagList(value any, tagMap map[string]string) any {
	return feature.RewriteOutboundTagList(value, tagMap)
}
func UnsyncConnectionFromOutbound(tx *gorm.DB, connection *RemoteOutboundConnection) error {
	return feature.UnsyncConnectionFromOutbound(sourceStore(tx), connection)
}
func DeleteSyncedOutboundsForSubscription(tx *gorm.DB, subscriptionID uint) (bool, error) {
	return feature.DeleteSyncedOutboundsForSubscription(sourceStore(tx), subscriptionID)
}
func ReconcileOutboundLinks(tx *gorm.DB) (bool, error) {
	return feature.ReconcileOutboundLinks(sourceStore(tx))
}
func RetagSubscriptionConnections(tx *gorm.DB, subscription *RemoteOutboundSubscription) (bool, error) {
	return feature.RetagSubscriptionConnections(sourceStore(tx), subscription)
}
func UniqueOutboundTag(tx *gorm.DB, subscription RemoteOutboundSubscription, name string, connectionID uint) (string, error) {
	return feature.UniqueOutboundTag(sourceStore(tx), subscription, name, connectionID)
}
func OutboundTagAvailable(tx *gorm.DB, tag string, connectionID uint) (bool, error) {
	return feature.OutboundTagAvailable(sourceStore(tx), tag, connectionID)
}
func groupCheckTags(connection RemoteOutboundConnection, connections []RemoteOutboundConnection, tagMap map[string]string) []string {
	return feature.GroupCheckTags(connection, connections, tagMap)
}
func groupCheckConnections(tx *gorm.DB, connection RemoteOutboundConnection) ([]RemoteOutboundConnection, error) {
	return feature.GroupCheckConnections(sourceStore(tx), connection)
}
func remoteConnectionIsGroup(connection RemoteOutboundConnection) bool {
	return feature.ConnectionIsGroup(connection)
}
