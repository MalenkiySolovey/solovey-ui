package resources

import (
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
)

const InboundTransportCapabilitySchemaV2 = canonical.InboundTransportCapabilitySchemaV2
const RuntimeBuildFeatureSchemaV1 = canonical.RuntimeBuildFeatureSchemaV1
const MaxInboundTransportFactsV2 = canonical.MaxInboundTransportFactsV2
const MaxInboundTransportReasonsV2 = canonical.MaxInboundTransportReasonsV2
const MaxInboundTransportFreshnessV2 = canonical.MaxInboundTransportFreshnessV2

type InboundTransportClass = canonical.InboundTransportClass

const TransportPlainUDP = canonical.TransportPlainUDP
const TransportQUICNative = canonical.TransportQUICNative
const TransportQUICV2Ray = canonical.TransportQUICV2Ray
const TransportTCPUDPDual = canonical.TransportTCPUDPDual
const TransportProxyUDPAssociation = canonical.TransportProxyUDPAssociation
const TransportDNSServiceUnknown = canonical.TransportDNSServiceUnknown
const TransportLocalProxy = canonical.TransportLocalProxy
const TransportInterception = canonical.TransportInterception
const TransportExternalManaged = canonical.TransportExternalManaged
const TransportUnsupported = canonical.TransportUnsupported

type RuntimeFeatureState = canonical.RuntimeFeatureState

const RuntimeFeatureSupported = canonical.RuntimeFeatureSupported
const RuntimeFeatureUnavailable = canonical.RuntimeFeatureUnavailable
const RuntimeFeatureUnknown = canonical.RuntimeFeatureUnknown

type RuntimeBuildFeatureV1 = canonical.RuntimeBuildFeatureV1
type InboundTransportCapabilityV2 = canonical.InboundTransportCapabilityV2

func FinalizeRuntimeBuildFeatureV1(value RuntimeBuildFeatureV1) RuntimeBuildFeatureV1 {
	return canonical.FinalizeRuntimeBuildFeatureV1(value)
}

func FinalizeInboundTransportCapabilityV2(value InboundTransportCapabilityV2) InboundTransportCapabilityV2 {
	return canonical.FinalizeInboundTransportCapabilityV2(value)
}

func boundedToken(value string, limit int) bool {
	return canonical.TransportContractTokenValidV2(value, limit)
}
