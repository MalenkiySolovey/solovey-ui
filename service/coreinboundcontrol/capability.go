package coreinboundcontrol

import (
	"sort"
	"strings"
)

func ResolveNativeFallbackCapabilityV1(input CapabilityInputV1) NativeFallbackCapabilityV1 {
	result := NativeFallbackCapabilityV1{
		Disposition: CapabilityUnknown, Variant: NativeFallbackNone,
		CapabilityResolverRevision: CapabilityResolverRevisionV1,
	}
	inboundType := strings.ToLower(strings.TrimSpace(input.InboundType))
	if !input.ShapeKnown {
		result.ReasonCodes = []ReasonCode{ReasonInboundShapeUnknown}
		return result
	}
	if input.Listener.Port == 0 {
		result.ReasonCodes = []ReasonCode{ReasonListenerPortInvalid}
		return result
	}
	switch inboundType {
	case "hysteria", "hysteria2", "tuic", "quic":
		result.Disposition = CapabilityOutOfScope
		result.ReasonCodes = []ReasonCode{ReasonProtocolOutOfScope}
		return result
	case "vless":
		return resolveVLESSCapability(input, result)
	case "trojan":
		return resolveTrojanCapability(input, result)
	default:
		result.Disposition = CapabilityUnsupported
		result.ReasonCodes = []ReasonCode{ReasonInboundTypeUnsupported}
		return result
	}
}

func resolveVLESSCapability(input CapabilityInputV1, result NativeFallbackCapabilityV1) NativeFallbackCapabilityV1 {
	if !input.TLS.Enabled {
		result.Disposition = CapabilityUnsupported
		result.ReasonCodes = []ReasonCode{ReasonVLESSFallbackUnavailable}
		return result
	}
	if !input.TLS.Reality.Enabled {
		result.Disposition = CapabilityUnsupported
		result.ReasonCodes = []ReasonCode{ReasonVLESSOrdinaryTLSUnsupported}
		return result
	}
	if input.Listener.Network != "tcp" {
		result.Disposition = CapabilityUnsupported
		result.ReasonCodes = []ReasonCode{ReasonListenerNetworkUnknown}
		return result
	}
	if input.Transport.Present {
		result.Disposition = CapabilityUnsupported
		result.ReasonCodes = []ReasonCode{ReasonV2RayTransportUnsupported}
		return result
	}
	if input.TLS.Reality.Handshake.Kind != "tcp_host_port" {
		result.Disposition = CapabilityUnsupported
		result.ReasonCodes = []ReasonCode{ReasonRealityTargetNotTCP}
		return result
	}
	if !runtimeIdentityVerified(input.Runtime) {
		result.ReasonCodes = []ReasonCode{ReasonRuntimeIdentityUnverified}
		return result
	}
	if !input.Runtime.WithUTLS {
		result.Disposition = CapabilityUnsupported
		result.ReasonCodes = []ReasonCode{ReasonWithUTLSRequired}
		return result
	}
	result.Disposition = CapabilitySupportedNaturalFallback
	result.Variant = NativeFallbackVLESSRealityTCP
	result.NaturalInvalidTrafficFallback = true
	return result
}

func resolveTrojanCapability(input CapabilityInputV1, result NativeFallbackCapabilityV1) NativeFallbackCapabilityV1 {
	if input.Listener.Network != "tcp" {
		result.Disposition = CapabilityUnsupported
		result.ReasonCodes = []ReasonCode{ReasonListenerNetworkUnknown}
		return result
	}
	if input.Transport.Present {
		result.Disposition = CapabilityUnsupported
		result.ReasonCodes = []ReasonCode{ReasonV2RayTransportUnsupported}
		return result
	}
	if input.Multiplex.Present {
		result.Disposition = CapabilityUnsupported
		result.ReasonCodes = []ReasonCode{ReasonTrojanMultiplexUnproven}
		return result
	}
	if !input.TLS.Enabled {
		if len(input.ALPNFallbacks) > 0 {
			result.Disposition = CapabilityUnsupported
			result.ReasonCodes = []ReasonCode{ReasonTrojanTLSRequired}
			return result
		}
		result.Disposition = CapabilityNotShipped
		result.ReasonCodes = []ReasonCode{ReasonTrojanTLSRequired}
		return result
	}
	if !input.DefaultFallback.Present && len(input.ALPNFallbacks) == 0 {
		result.Disposition = CapabilityUnsupported
		result.ReasonCodes = []ReasonCode{ReasonTrojanFallbackNotConfigured}
		return result
	}
	if input.DefaultFallback.Present && input.DefaultFallback.Kind != "tcp_host_port" {
		result.Disposition = CapabilityUnsupported
		result.ReasonCodes = []ReasonCode{ReasonFallbackTargetNotTCP}
		return result
	}
	if len(input.ALPNFallbacks) > 0 {
		if len(input.TLS.ALPN) == 0 {
			result.Disposition = CapabilityUnsupported
			result.ReasonCodes = []ReasonCode{ReasonTrojanALPNSetUnknown}
			return result
		}
		if !exhaustiveALPN(input.TLS.ALPN, input.ALPNFallbacks) {
			result.Disposition = CapabilityUnsupported
			result.ReasonCodes = []ReasonCode{ReasonTrojanALPNNotExhaustive}
			return result
		}
	}
	if !runtimeIdentityVerified(input.Runtime) {
		result.ReasonCodes = []ReasonCode{ReasonRuntimeIdentityUnverified}
		return result
	}
	result.Disposition = CapabilitySupported
	result.NaturalInvalidTrafficFallback = true
	switch {
	case input.DefaultFallback.Present && len(input.ALPNFallbacks) > 0:
		result.Variant = NativeFallbackTrojanDefaultALPNTCP
	case len(input.ALPNFallbacks) > 0:
		result.Variant = NativeFallbackTrojanALPNTCP
	default:
		result.Variant = NativeFallbackTrojanDefaultTCP
	}
	return result
}

func exhaustiveALPN(admitted []string, fallbacks []ALPNFallbackShapeV1) bool {
	for _, value := range admitted {
		if !safeALPN(value) {
			return false
		}
	}
	expected := exactStrings(admitted)
	actual := make([]string, 0, len(fallbacks))
	for _, fallback := range fallbacks {
		if !safeALPN(fallback.ALPN) || !fallback.Target.Present || fallback.Target.Kind != "tcp_host_port" {
			return false
		}
		actual = append(actual, fallback.ALPN)
	}
	actual = exactStrings(actual)
	if len(expected) != len(actual) {
		return false
	}
	for index := range expected {
		if expected[index] != actual[index] {
			return false
		}
	}
	return true
}

func exactStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func safeALPN(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 64 ||
		strings.ContainsAny(value, "\\:?#&={}[]<>\"'\r\n\t ") || strings.HasPrefix(value, "/") ||
		strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") || strings.Contains(value, "/./") ||
		strings.Contains(value, "/../") || strings.HasSuffix(value, "/.") || strings.HasSuffix(value, "/..") {
		return false
	}
	return true
}

func runtimeIdentityVerified(identity CoreRuntimeIdentityV1) bool {
	return identity.Schema == RuntimeIdentitySchemaV1 && identity.State == RuntimeIdentityVerified && len(identity.ReasonCodes) == 0 &&
		identity.SingBoxModule == PinnedSingBoxModule && identity.SingBoxVersion == PinnedSingBoxVersion && identity.SingBoxModuleSum == PinnedSingBoxModuleSum &&
		identity.SingBoxSourceRevision == PinnedSingBoxSourceRevision && identity.UTLSModule == PinnedUTLSModule &&
		identity.UTLSVersion == PinnedUTLSVersion && identity.UTLSModuleSum == PinnedUTLSModuleSum && identity.UTLSSourceRevision == PinnedUTLSSourceRevision &&
		identity.BuildProfileRevision == expectedBuildProfileRevision(identity.WithUTLS) &&
		identity.CapabilityResolverRevision == CapabilityResolverRevisionV1 && identity.IdentityRevision != ""
}
