package coreinboundcontrol

import (
	canonical "github.com/MalenkiySolovey/solovey-host/coreruntime/inboundcontrol"
)

const (
	RuntimeIdentitySchemaV1  = canonical.RuntimeIdentitySchemaV1
	InboundSnapshotSchemaV1  = canonical.InboundSnapshotSchemaV1
	QUICBuildFeatureSchemaV1 = canonical.QUICBuildFeatureSchemaV1

	PinnedSingBoxModule         = "github.com/sagernet/sing-box"
	PinnedSingBoxVersion        = "v1.14.2"
	PinnedSingBoxModuleSum      = "h1:uZX7iz70o7lzoltR6sElMYP7kI7PVf23r4X0HQSozXM="
	PinnedSingBoxSourceRevision = "af6e64c3b69e6132ebaee0e1a3d24e93903f6709"
	PinnedUTLSModule            = "github.com/metacubex/utls"
	PinnedUTLSVersion           = "v1.8.7"
	PinnedUTLSModuleSum         = "h1:Cp+yWkNTFkSihETgGWq34hlVFds5HpYWVOR1xovUVTs="
	PinnedUTLSSourceRevision    = "f7d52c22f3a8d2f510ad1470f75cb6c3fe26aa37"

	CapabilityResolverRevisionV1            = "cfbda64b8f432625e892de52080d54215dcd5c5a03af9701fb53825f88b0c771"
	BuildProfileWithUTLSRevision            = "2573ebf675608c4e589ebc062ecf46078f77244489cb5a1578ecb2b649ae0608"
	BuildProfileWithoutUTLSRevision         = "a55ee5ad8f16d0ab527a612e4a89e2170b2751af200430a90022df99cc33192d"
	PinnedRuntimeIdentityWithUTLSRevisionV1 = "4fedb2d7d6cdf06fed0bee755311eb053bb8e611b262d7fadc839f43c22efc25"
)

type ReasonCode = canonical.ReasonCode

const (
	ReasonBuildInfoMissing               = canonical.ReasonBuildInfoMissing
	ReasonSingBoxModuleMissing           = canonical.ReasonSingBoxModuleMissing
	ReasonSingBoxModuleReplaced          = canonical.ReasonSingBoxModuleReplaced
	ReasonSingBoxVersionMismatch         = canonical.ReasonSingBoxVersionMismatch
	ReasonSingBoxSumMismatch             = canonical.ReasonSingBoxSumMismatch
	ReasonSingBoxModuleAmbiguous         = canonical.ReasonSingBoxModuleAmbiguous
	ReasonSingBoxSourceUnqualified       = canonical.ReasonSingBoxSourceUnqualified
	ReasonUTLSModuleMissing              = canonical.ReasonUTLSModuleMissing
	ReasonUTLSModuleReplaced             = canonical.ReasonUTLSModuleReplaced
	ReasonUTLSVersionMismatch            = canonical.ReasonUTLSVersionMismatch
	ReasonUTLSSumMismatch                = canonical.ReasonUTLSSumMismatch
	ReasonUTLSModuleAmbiguous            = canonical.ReasonUTLSModuleAmbiguous
	ReasonBuildProfileUnknown            = canonical.ReasonBuildProfileUnknown
	ReasonWithUTLSInconsistent           = canonical.ReasonWithUTLSInconsistent
	ReasonResolverRevisionMismatch       = canonical.ReasonResolverRevisionMismatch
	ReasonRuntimeIdentityUnverified      = canonical.ReasonRuntimeIdentityUnverified
	ReasonInboundOptionsMalformed        = canonical.ReasonInboundOptionsMalformed
	ReasonTLSReferenceMissing            = canonical.ReasonTLSReferenceMissing
	ReasonTLSReferenceMismatch           = canonical.ReasonTLSReferenceMismatch
	ReasonTLSOptionsMalformed            = canonical.ReasonTLSOptionsMalformed
	ReasonInboundShapeUnknown            = canonical.ReasonInboundShapeUnknown
	ReasonListenerNetworkUnknown         = canonical.ReasonListenerNetworkUnknown
	ReasonListenerPortInvalid            = canonical.ReasonListenerPortInvalid
	ReasonTagUnsafe                      = canonical.ReasonTagUnsafe
	ReasonVLESSFallbackUnavailable       = canonical.ReasonVLESSFallbackUnavailable
	ReasonVLESSOrdinaryTLSUnsupported    = canonical.ReasonVLESSOrdinaryTLSUnsupported
	ReasonRealityRequired                = canonical.ReasonRealityRequired
	ReasonRealityTargetNotTCP            = canonical.ReasonRealityTargetNotTCP
	ReasonFallbackTargetNotTCP           = canonical.ReasonFallbackTargetNotTCP
	ReasonWithUTLSRequired               = canonical.ReasonWithUTLSRequired
	ReasonV2RayTransportUnsupported      = canonical.ReasonV2RayTransportUnsupported
	ReasonTrojanTLSRequired              = canonical.ReasonTrojanTLSRequired
	ReasonTrojanFallbackNotConfigured    = canonical.ReasonTrojanFallbackNotConfigured
	ReasonTrojanALPNSetUnknown           = canonical.ReasonTrojanALPNSetUnknown
	ReasonTrojanALPNNotExhaustive        = canonical.ReasonTrojanALPNNotExhaustive
	ReasonTrojanMultiplexUnproven        = canonical.ReasonTrojanMultiplexUnproven
	ReasonProtocolOutOfScope             = canonical.ReasonProtocolOutOfScope
	ReasonInboundTypeUnsupported         = canonical.ReasonInboundTypeUnsupported
	ReasonEffectiveRuntimeUnavailable    = canonical.ReasonEffectiveRuntimeUnavailable
	ReasonEffectiveInboundMissing        = canonical.ReasonEffectiveInboundMissing
	ReasonEffectiveInboundTypeMismatch   = canonical.ReasonEffectiveInboundTypeMismatch
	ReasonEffectiveConfigurationUnproven = canonical.ReasonEffectiveConfigurationUnproven
)

type RuntimeIdentityState = canonical.RuntimeIdentityState

const (
	RuntimeIdentityVerified = canonical.RuntimeIdentityVerified
	RuntimeIdentityUnknown  = canonical.RuntimeIdentityUnknown
)

type BuildFeatureState = canonical.BuildFeatureState

const (
	BuildFeatureSupported   = canonical.BuildFeatureSupported
	BuildFeatureUnavailable = canonical.BuildFeatureUnavailable
	BuildFeatureUnknown     = canonical.BuildFeatureUnknown
)

type QUICBuildFeatureV1 = canonical.QUICBuildFeatureV1

type CoreRuntimeIdentityV1 = canonical.CoreRuntimeIdentityV1

type RuntimeModuleV1 = canonical.RuntimeModuleV1

type RuntimeBuildInputV1 = canonical.RuntimeBuildInputV1

type ListenerShapeV1 = canonical.ListenerShapeV1

type AuthenticationShapeV1 = canonical.AuthenticationShapeV1

type LocalProxyShapeV1 = canonical.LocalProxyShapeV1

// InterceptionShapeV1 is the secret-free, pinned-core truth needed by a
// neutral interception consumer. It describes only an existing core-owned
// Redirect/TProxy inbound; it never grants authority to mutate that inbound or
// supplies firewall/routing values.
type InterceptionShapeV1 = canonical.InterceptionShapeV1

type UDPTransportShapeV2 = canonical.UDPTransportShapeV2

type TransportShapeV1 = canonical.TransportShapeV1

type MultiplexShapeV1 = canonical.MultiplexShapeV1

type TargetShapeV1 = canonical.TargetShapeV1

type RealityShapeV1 = canonical.RealityShapeV1

type TLSShapeV1 = canonical.TLSShapeV1

type ALPNFallbackShapeV1 = canonical.ALPNFallbackShapeV1

type EffectiveInboundV1 = canonical.EffectiveInboundV1

type CapabilityDisposition = canonical.CapabilityDisposition

const (
	CapabilitySupportedNaturalFallback = canonical.CapabilitySupportedNaturalFallback
	CapabilitySupported                = canonical.CapabilitySupported
	CapabilityUnsupported              = canonical.CapabilityUnsupported
	CapabilityNotShipped               = canonical.CapabilityNotShipped
	CapabilityOutOfScope               = canonical.CapabilityOutOfScope
	CapabilityUnknown                  = canonical.CapabilityUnknown
)

type NativeFallbackVariant = canonical.NativeFallbackVariant

const (
	NativeFallbackNone                 = canonical.NativeFallbackNone
	NativeFallbackVLESSRealityTCP      = canonical.NativeFallbackVLESSRealityTCP
	NativeFallbackTrojanDefaultTCP     = canonical.NativeFallbackTrojanDefaultTCP
	NativeFallbackTrojanALPNTCP        = canonical.NativeFallbackTrojanALPNTCP
	NativeFallbackTrojanDefaultALPNTCP = canonical.NativeFallbackTrojanDefaultALPNTCP
)

type NativeFallbackCapabilityV1 = canonical.NativeFallbackCapabilityV1

type CapabilityInputV1 = canonical.CapabilityInputV1

type InboundFallbackSnapshotV1 = canonical.InboundFallbackSnapshotV1
