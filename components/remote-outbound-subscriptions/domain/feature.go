//go:build !minimal

package remote

import (
	feature "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions/source/domain"
	"gorm.io/gorm"
)

type RemoteOutboundSubscription = feature.RemoteOutboundSubscription
type RemoteOutboundGroup = feature.RemoteOutboundGroup
type RemoteOutboundGroupConnection = feature.RemoteOutboundGroupConnection
type RemoteOutboundConnection = feature.RemoteOutboundConnection
type NormalizedOutbound = feature.NormalizedOutbound

var NormalizeOutbound = feature.NormalizeOutbound
var NormalizeOutboundWithCanonical = feature.NormalizeOutboundWithCanonical
var UpdateConnection = feature.UpdateConnection
var SourceKey = feature.SourceKey
var UniqueSourceKey = feature.UniqueSourceKey
var DisplayName = feature.DisplayName
var PortString = feature.PortString
var CloneOutboundMap = feature.CloneOutboundMap
var JSONRawEqual = feature.JSONRawEqual
var DefaultTagPrefix = feature.DefaultTagPrefix
var SanitizeTagPrefix = feature.SanitizeTagPrefix
var SanitizeTagPart = feature.SanitizeTagPart
var SanitizeTagText = feature.SanitizeTagText
var TrimTag = feature.TrimTag
var NormalizeUpdateInterval = feature.NormalizeUpdateInterval

type ClientLink = feature.ClientLink
type ClientConversionOptions = feature.ClientConversionOptions

var ConnectionOutboundConfig = feature.ConnectionOutboundConfig
var HydrateConnectionTypeInfo = feature.HydrateConnectionTypeInfo
var ApplySourceFilters = feature.ApplySourceFilters
var canonicalConnection = feature.CanonicalConnection
var legacyTypedLabelSourceKey = feature.LegacyTypedLabelSourceKey
var sourceDisplayName = feature.SourceDisplayName
var connectionOutboundConfig = feature.ConnectionOutboundConfigWithTags
var connectionOutboundMapForClient = feature.ConnectionOutboundMapForClient
var applyClientConversion = feature.ApplyClientConversion
var stringList = feature.StringList
var cloneOutboundMap = feature.CloneOutboundMap
var ErrSourceFilterInvalid = feature.ErrSourceFilterInvalid
var ErrSourceFilterDependency = feature.ErrSourceFilterDependency
var rewriteOutboundTagReferences = feature.RewriteOutboundTagReferences

const SourceRemote = feature.SourceRemote
const SourceInline = feature.SourceInline
const SourceFile = feature.SourceFile
const MaxInlineSourceBytes = feature.MaxInlineSourceBytes

var ErrSourceContentInvalid = feature.ErrSourceContentInvalid

type FormatResult = feature.FormatResult
type FetchAttempt = feature.FetchAttempt
type FetchedSubscription = feature.FetchedSubscription
type CollectionSnapshot = feature.CollectionSnapshot
type clientLinkSelection struct {
	Kind string
	ID   uint
}
type RefreshResult = feature.RefreshResult
type GroupActionResult = feature.GroupActionResult

const DefaultGroupName = feature.DefaultGroupName

var FilterVisibleConnections = feature.FilterVisibleConnections
var errRefreshIdentityAmbiguous = feature.ErrRefreshIdentityAmbiguous

type SourceHealthCheck = feature.SourceHealthCheck

func BackendStore(db *gorm.DB) feature.Store { return sourceStore(db) }

type CheckResult = feature.CheckResult
type CheckOutboundResult = feature.CheckOutboundResult
