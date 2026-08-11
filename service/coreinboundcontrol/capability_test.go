package coreinboundcontrol

import "testing"

func baseCapabilityInput(inboundType string) CapabilityInputV1 {
	return CapabilityInputV1{
		Runtime: exactIdentity(true), ShapeKnown: true, InboundType: inboundType,
		Listener: ListenerShapeV1{Network: "tcp", AddressFamily: "ipv4", Bind: "0.0.0.0", Port: 443},
	}
}

func TestVLESSPlainAndOrdinaryTLSAreUnsupported(t *testing.T) {
	plain := ResolveNativeFallbackCapabilityV1(baseCapabilityInput("vless"))
	if plain.Disposition != CapabilityUnsupported || !containsReason(plain.ReasonCodes, ReasonVLESSFallbackUnavailable) {
		t.Fatalf("plain VLESS = %#v", plain)
	}
	tlsInput := baseCapabilityInput("vless")
	tlsInput.TLS.Enabled = true
	tlsResult := ResolveNativeFallbackCapabilityV1(tlsInput)
	if tlsResult.Disposition != CapabilityUnsupported || !containsReason(tlsResult.ReasonCodes, ReasonVLESSOrdinaryTLSUnsupported) {
		t.Fatalf("TLS VLESS = %#v", tlsResult)
	}
}

func TestPinnedTCPVLESSRealityRequiresWithUTLS(t *testing.T) {
	input := baseCapabilityInput("vless")
	input.TLS = TLSShapeV1{Enabled: true, Reality: RealityShapeV1{Present: true, Enabled: true, Handshake: TargetShapeV1{Present: true, Kind: "tcp_host_port"}}}
	result := ResolveNativeFallbackCapabilityV1(input)
	if result.Disposition != CapabilitySupportedNaturalFallback || result.Variant != NativeFallbackVLESSRealityTCP ||
		!result.NaturalInvalidTrafficFallback || result.ForcedSameSubjectDecoyRoute {
		t.Fatalf("VLESS REALITY = %#v", result)
	}
	input.Runtime = exactIdentity(false)
	result = ResolveNativeFallbackCapabilityV1(input)
	if result.Disposition != CapabilityUnsupported || !containsReason(result.ReasonCodes, ReasonWithUTLSRequired) {
		t.Fatalf("VLESS REALITY without with_utls = %#v", result)
	}
}

func TestVLESSRealityRejectsUnixTransportAndUnknownShape(t *testing.T) {
	input := baseCapabilityInput("vless")
	input.TLS = TLSShapeV1{Enabled: true, Reality: RealityShapeV1{Present: true, Enabled: true, Handshake: TargetShapeV1{Present: true, Kind: "unix"}}}
	if result := ResolveNativeFallbackCapabilityV1(input); result.Disposition != CapabilityUnsupported || !containsReason(result.ReasonCodes, ReasonRealityTargetNotTCP) {
		t.Fatalf("Unix target = %#v", result)
	}
	input.TLS.Reality.Handshake.Kind = "tcp_host_port"
	input.Transport = TransportShapeV1{Present: true, Type: "ws"}
	if result := ResolveNativeFallbackCapabilityV1(input); result.Disposition != CapabilityUnsupported || !containsReason(result.ReasonCodes, ReasonV2RayTransportUnsupported) {
		t.Fatalf("transport = %#v", result)
	}
	input.ShapeKnown = false
	if result := ResolveNativeFallbackCapabilityV1(input); result.Disposition != CapabilityUnknown {
		t.Fatalf("unknown shape = %#v", result)
	}
}

func TestTrojanTLSDefaultAndExhaustiveALPNAreSupported(t *testing.T) {
	input := baseCapabilityInput("trojan")
	input.TLS = TLSShapeV1{Enabled: true}
	input.DefaultFallback = TargetShapeV1{Present: true, Kind: "tcp_host_port"}
	result := ResolveNativeFallbackCapabilityV1(input)
	if result.Disposition != CapabilitySupported || result.Variant != NativeFallbackTrojanDefaultTCP || !result.NaturalInvalidTrafficFallback || result.ForcedSameSubjectDecoyRoute {
		t.Fatalf("Trojan default = %#v", result)
	}
	input.TLS.ALPN = []string{"http/1.1", "h2"}
	input.ALPNFallbacks = []ALPNFallbackShapeV1{
		{ALPN: "h2", Target: TargetShapeV1{Present: true, Kind: "tcp_host_port"}},
		{ALPN: "http/1.1", Target: TargetShapeV1{Present: true, Kind: "tcp_host_port"}},
	}
	result = ResolveNativeFallbackCapabilityV1(input)
	if result.Disposition != CapabilitySupported || result.Variant != NativeFallbackTrojanDefaultALPNTCP {
		t.Fatalf("Trojan ALPN = %#v", result)
	}
}

func TestTrojanALPNFailsClosed(t *testing.T) {
	input := baseCapabilityInput("trojan")
	input.TLS = TLSShapeV1{Enabled: true, ALPN: []string{"h2", "http/1.1"}}
	input.ALPNFallbacks = []ALPNFallbackShapeV1{{ALPN: "h2", Target: TargetShapeV1{Present: true, Kind: "tcp_host_port"}}}
	if result := ResolveNativeFallbackCapabilityV1(input); result.Disposition != CapabilityUnsupported || !containsReason(result.ReasonCodes, ReasonTrojanALPNNotExhaustive) {
		t.Fatalf("partial ALPN = %#v", result)
	}
	input.TLS.Enabled = false
	if result := ResolveNativeFallbackCapabilityV1(input); result.Disposition != CapabilityUnsupported || !containsReason(result.ReasonCodes, ReasonTrojanTLSRequired) {
		t.Fatalf("ALPN without TLS = %#v", result)
	}
}

func TestTrojanALPNCoverageUsesExactRuntimeKeys(t *testing.T) {
	input := baseCapabilityInput("trojan")
	input.TLS = TLSShapeV1{Enabled: true, ALPN: []string{"h2"}}
	input.ALPNFallbacks = []ALPNFallbackShapeV1{{ALPN: " h2", Target: TargetShapeV1{Present: true, Kind: "tcp_host_port"}}}
	result := ResolveNativeFallbackCapabilityV1(input)
	if result.Disposition != CapabilityUnsupported || !containsReason(result.ReasonCodes, ReasonTrojanALPNNotExhaustive) {
		t.Fatalf("non-exact ALPN map = %#v", result)
	}
}

func TestTrojanNonTLSTransportAndMultiplexAreNotAdmitted(t *testing.T) {
	nonTLS := ResolveNativeFallbackCapabilityV1(baseCapabilityInput("trojan"))
	if nonTLS.Disposition != CapabilityNotShipped {
		t.Fatalf("non-TLS Trojan = %#v", nonTLS)
	}
	input := baseCapabilityInput("trojan")
	input.TLS.Enabled = true
	input.DefaultFallback = TargetShapeV1{Present: true, Kind: "tcp_host_port"}
	input.Transport = TransportShapeV1{Present: true, Type: "grpc"}
	if result := ResolveNativeFallbackCapabilityV1(input); result.Disposition != CapabilityUnsupported {
		t.Fatalf("Trojan transport = %#v", result)
	}
	input.Transport = TransportShapeV1{}
	input.Multiplex = MultiplexShapeV1{Present: true, Enabled: true}
	if result := ResolveNativeFallbackCapabilityV1(input); result.Disposition != CapabilityUnsupported || !containsReason(result.ReasonCodes, ReasonTrojanMultiplexUnproven) {
		t.Fatalf("Trojan multiplex = %#v", result)
	}
}

func TestDatagramProtocolsAreOutOfScopeAndUnknownRuntimeBlocksAdmission(t *testing.T) {
	for _, inboundType := range []string{"hysteria", "hysteria2", "tuic", "quic"} {
		input := baseCapabilityInput(inboundType)
		input.Listener.Network = "udp"
		if result := ResolveNativeFallbackCapabilityV1(input); result.Disposition != CapabilityOutOfScope {
			t.Fatalf("%s = %#v", inboundType, result)
		}
	}
	input := baseCapabilityInput("vless")
	input.Runtime = ResolveRuntimeIdentityV1(RuntimeBuildInputV1{})
	input.TLS = TLSShapeV1{Enabled: true, Reality: RealityShapeV1{Present: true, Enabled: true, Handshake: TargetShapeV1{Present: true, Kind: "tcp_host_port"}}}
	if result := ResolveNativeFallbackCapabilityV1(input); result.Disposition != CapabilityUnknown || !containsReason(result.ReasonCodes, ReasonRuntimeIdentityUnverified) {
		t.Fatalf("unknown runtime = %#v", result)
	}
}
