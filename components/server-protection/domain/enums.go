package domain

import "fmt"

const ClassifierPolicyVersion = 1

const (
	DefaultScoreThreshold     = 5
	DefaultGraylistTTLSeconds = 3600
	DefaultMaxScore           = 100
	DefaultIPv6PrefixBits     = 64
	DefaultSafeMetaMaxBytes   = 512
)

type ProfileMode string

const (
	ProfileModeRecordOnly      ProfileMode = "record_only"
	ProfileModeMetadataOnly    ProfileMode = "metadata_only"
	ProfileModePassiveFirewall ProfileMode = "passive_firewall"
	ProfileModeSelfSteal       ProfileMode = "self_steal"
	ProfileModeNativeFallback  ProfileMode = "native_fallback"
	ProfileModeFrontedTLS      ProfileMode = "fronted_tls"
	ProfileModeFrontedStream   ProfileMode = "fronted_stream"
)

func (value ProfileMode) Validate() error {
	switch value {
	case ProfileModeRecordOnly, ProfileModeMetadataOnly, ProfileModePassiveFirewall,
		ProfileModeSelfSteal, ProfileModeNativeFallback, ProfileModeFrontedTLS, ProfileModeFrontedStream:
		return nil
	default:
		return invalidEnum("profile mode", string(value))
	}
}

type SignalKind string

const (
	SignalHTTPScannerPath       SignalKind = "http_scanner_path"
	SignalHTTPEmptyUA           SignalKind = "http_empty_ua"
	SignalHTTPScannerUA         SignalKind = "http_scanner_ua"
	SignalHTTPUnexpectedMethod  SignalKind = "http_unexpected_method"
	SignalRateLimited           SignalKind = "rate_limited"
	SignalResourceDrift         SignalKind = "resource_drift"
	SignalMissingSNI            SignalKind = "missing_sni"
	SignalUnexpectedALPN        SignalKind = "unexpected_alpn"
	SignalFallbackHit           SignalKind = "fallback_hit"
	SignalShortTLSSession       SignalKind = "short_tls_session"
	SignalSYNBurst              SignalKind = "syn_burst"
	SignalConntrackPressure     SignalKind = "conntrack_pressure"
	SignalRealClientCorrelation SignalKind = "real_client_correlation"
	SignalTinyTransfer          SignalKind = "tiny_transfer"
	SignalExternalReputation    SignalKind = "external_reputation"
)

func (value SignalKind) Validate() error {
	switch value {
	case SignalHTTPScannerPath, SignalHTTPEmptyUA, SignalHTTPScannerUA,
		SignalHTTPUnexpectedMethod, SignalRateLimited, SignalResourceDrift,
		SignalMissingSNI, SignalUnexpectedALPN, SignalFallbackHit,
		SignalShortTLSSession, SignalSYNBurst, SignalConntrackPressure,
		SignalRealClientCorrelation, SignalTinyTransfer, SignalExternalReputation:
		return nil
	default:
		return invalidEnum("signal kind", string(value))
	}
}

func DefaultSignalDelta(kind SignalKind) int {
	switch kind {
	case SignalHTTPScannerPath, SignalMissingSNI, SignalSYNBurst:
		return 3
	case SignalHTTPEmptyUA:
		return 1
	case SignalHTTPScannerUA, SignalHTTPUnexpectedMethod, SignalRateLimited,
		SignalUnexpectedALPN, SignalFallbackHit, SignalShortTLSSession, SignalTinyTransfer:
		return 2
	case SignalConntrackPressure:
		return 4
	case SignalRealClientCorrelation:
		return 5
	case SignalExternalReputation:
		return 2
	case SignalResourceDrift:
		return 0
	default:
		return 0
	}
}

type DecisionAction string

const (
	DecisionRecordOnly   DecisionAction = "record_only"
	DecisionRateLimit    DecisionAction = "rate_limit"
	DecisionRouteToDecoy DecisionAction = "route_to_decoy"
	DecisionBlock        DecisionAction = "block"
)

func (value DecisionAction) Validate() error {
	switch value {
	case DecisionRecordOnly, DecisionRateLimit, DecisionRouteToDecoy, DecisionBlock:
		return nil
	default:
		return invalidEnum("decision action", string(value))
	}
}

type ResourceKind string

const (
	ResourcePanelWeb          ResourceKind = "panel_web"
	ResourceSubscription      ResourceKind = "subscription"
	ResourceInbound           ResourceKind = "inbound"
	ResourcePublicSite        ResourceKind = "public_site"
	ResourceComponentListener ResourceKind = "component_listener"
	ResourceNodeControl       ResourceKind = "node_control"
	ResourceNodeListener      ResourceKind = "node_listener"
)

func (value ResourceKind) Validate() error {
	switch value {
	case ResourcePanelWeb, ResourceSubscription, ResourceInbound, ResourcePublicSite,
		ResourceComponentListener, ResourceNodeControl, ResourceNodeListener:
		return nil
	default:
		return invalidEnum("resource kind", string(value))
	}
}

type Protocol string

const (
	ProtocolTCP     Protocol = "tcp"
	ProtocolUDP     Protocol = "udp"
	ProtocolHTTP    Protocol = "http"
	ProtocolHTTPS   Protocol = "https"
	ProtocolStream  Protocol = "stream"
	ProtocolQUIC    Protocol = "quic"
	ProtocolNodeRPC Protocol = "node_rpc"
)

func (value Protocol) Validate() error {
	switch value {
	case ProtocolTCP, ProtocolUDP, ProtocolHTTP, ProtocolHTTPS, ProtocolStream,
		ProtocolQUIC, ProtocolNodeRPC:
		return nil
	default:
		return invalidEnum("protocol", string(value))
	}
}

type OperationState string

const (
	OperationPrepared       OperationState = "prepared"
	OperationAbandoned      OperationState = "abandoned"
	OperationApplying       OperationState = "applying"
	OperationApplied        OperationState = "applied"
	OperationHealthFailed   OperationState = "health_failed"
	OperationRollingBack    OperationState = "rolling_back"
	OperationRolledBack     OperationState = "rolled_back"
	OperationRollbackFailed OperationState = "rollback_failed"
)

func (value OperationState) Validate() error {
	switch value {
	case OperationPrepared, OperationAbandoned, OperationApplying, OperationApplied,
		OperationHealthFailed, OperationRollingBack, OperationRolledBack, OperationRollbackFailed:
		return nil
	default:
		return invalidEnum("operation state", string(value))
	}
}

type FirewallBackend string

const (
	FirewallPreviewOnly       FirewallBackend = "preview_only"
	FirewallUnsupported       FirewallBackend = "unsupported"
	FirewallNFTables          FirewallBackend = "nftables"
	FirewallIPTables          FirewallBackend = "iptables"
	FirewallExternalFirewalld FirewallBackend = "firewalld_external"
	FirewallExternalUFW       FirewallBackend = "ufw_external"
)

func (value FirewallBackend) Validate() error {
	switch value {
	case FirewallPreviewOnly, FirewallUnsupported, FirewallNFTables, FirewallIPTables,
		FirewallExternalFirewalld, FirewallExternalUFW:
		return nil
	default:
		return invalidEnum("firewall backend", string(value))
	}
}

type SupportState string

const (
	SupportSupported         SupportState = "supported"
	SupportUnsupported       SupportState = "unsupported"
	SupportDegraded          SupportState = "degraded"
	SupportExternalManaged   SupportState = "external_managed"
	SupportMissingCapability SupportState = "missing_capability"
	SupportVersionSkew       SupportState = "version_skew"
)

func (value SupportState) Validate() error {
	switch value {
	case SupportSupported, SupportUnsupported, SupportDegraded, SupportExternalManaged,
		SupportMissingCapability, SupportVersionSkew:
		return nil
	default:
		return invalidEnum("support state", string(value))
	}
}

func invalidEnum(name, value string) error {
	return fmt.Errorf("invalid %s %q", name, value)
}
