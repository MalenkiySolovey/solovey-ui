package coreinboundcontrol

import (
	"context"
	"encoding/json"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	coreregistry "github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	sb "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/auth"
)

type EffectiveInboundReader interface {
	LookupInbound(tag string) (runtimeAvailable bool, inboundType string, inboundTag string, present bool)
}

type ExactEffectiveInboundReader interface {
	LookupInboundExact(tag string) (runtimeAvailable bool, inboundType, inboundTag, optionsDigest string, managerGeneration uint64, present bool)
}

func buildSnapshot(inbound model.Inbound, tlsReferenceCount int64, identity CoreRuntimeIdentityV1, effectiveReader EffectiveInboundReader, principalCounts ...int) InboundFallbackSnapshotV1 {
	principalCount := 0
	if len(principalCounts) != 0 && principalCounts[0] > 0 {
		principalCount = principalCounts[0]
	}
	expectedRuntimeDigest := ""
	if content, err := inbound.MarshalJSON(); err == nil {
		expectedRuntimeDigest, _ = canonicalInboundOptionsDigest(context.Background(), content)
	}
	return buildSnapshotWithRuntimeDigest(inbound, tlsReferenceCount, identity, effectiveReader, principalCount, expectedRuntimeDigest)
}

func buildSnapshotWithRuntimeDigest(inbound model.Inbound, tlsReferenceCount int64, identity CoreRuntimeIdentityV1, effectiveReader EffectiveInboundReader, principalCount int, expectedRuntimeDigest string) InboundFallbackSnapshotV1 {
	reasons := make([]ReasonCode, 0, 8)
	resourceID := inboundResourceID(inbound.Id, inbound.Tag)
	safeTag := safeTag(inbound.Tag)
	if strings.TrimSpace(inbound.Tag) != "" && safeTag == "" {
		reasons = append(reasons, ReasonTagUnsafe)
	}
	optionsContent := inbound.Options
	if len(strings.TrimSpace(string(optionsContent))) == 0 {
		optionsContent = json.RawMessage(`{}`)
	}
	optionsDigest, optionsErr := canonicalDigest(optionsContent)
	if optionsErr != nil {
		optionsDigest = digestBytes(optionsContent)
		reasons = append(reasons, ReasonInboundOptionsMalformed)
	}

	snapshot := InboundFallbackSnapshotV1{
		Schema: InboundSnapshotSchemaV1, InboundDatabaseID: inbound.Id, ResourceID: resourceID,
		Tag: safeTag, Type: normalizeType(inbound.Type), InboundOptionsDigest: optionsDigest,
		RuntimeIdentityRevision: identity.IdentityRevision, CapabilityResolverRevision: CapabilityResolverRevisionV1,
		TLSReferenceCount: tlsReferenceCount,
	}
	if inbound.TlsId != 0 {
		snapshot.TLSRecordDatabaseID = inbound.TlsId
		snapshot.TLSResourceID = "core:tls:" + strconv.FormatUint(uint64(inbound.TlsId), 10)
		snapshot.TLS.Referenced = true
		if inbound.Tls == nil {
			reasons = append(reasons, ReasonTLSReferenceMissing)
		} else {
			if inbound.Tls.Id != inbound.TlsId {
				reasons = append(reasons, ReasonTLSReferenceMismatch)
			}
			tlsContent := inbound.Tls.Server
			tlsDigest, tlsErr := canonicalDigest(tlsContent)
			if tlsErr != nil {
				tlsDigest = digestBytes(tlsContent)
				reasons = append(reasons, ReasonTLSOptionsMalformed)
			}
			snapshot.TLSOptionsDigest = tlsDigest
		}
	} else if inbound.Tls != nil {
		reasons = append(reasons, ReasonTLSReferenceMismatch)
	}

	shapeKnown := optionsErr == nil && !containsReason(reasons, ReasonTLSReferenceMissing) &&
		!containsReason(reasons, ReasonTLSReferenceMismatch) && !containsReason(reasons, ReasonTLSOptionsMalformed)
	if shapeKnown {
		if err := populateTypedShapes(&snapshot, inbound); err != nil {
			shapeKnown = false
			reasons = append(reasons, ReasonInboundShapeUnknown)
		}
		if snapshot.TLS.Enabled && !snapshot.TLS.Referenced {
			shapeKnown = false
			reasons = append(reasons, ReasonTLSReferenceMismatch)
		}
	}
	if snapshot.Authentication.Count < principalCount {
		snapshot.Authentication.Count = principalCount
	}
	if snapshot.Authentication.Count > 0 {
		snapshot.Authentication.Known, snapshot.Authentication.Expected = true, true
	}
	snapshot.Authentication.Revision = digestValue(struct {
		Schema         string
		Known, Present bool
		Count          int
	}{"solovey-ui/inbound-auth-shape/v2", snapshot.Authentication.Known, snapshot.Authentication.Expected, snapshot.Authentication.Count})
	finalizeLocalProxyShape(&snapshot)
	finalizeUDPTransportShape(&snapshot, identity)
	finalizeInterceptionShape(&snapshot)
	if snapshot.Listener.Network == "" {
		snapshot.Listener.Network = listenerNetworkForType(snapshot.Type)
	}
	if snapshot.Listener.Network == "unknown" {
		reasons = append(reasons, ReasonListenerNetworkUnknown)
	}
	if snapshot.Listener.Port == 0 {
		reasons = append(reasons, ReasonListenerPortInvalid)
	}
	snapshot.Effective = effectiveInbound(inbound.Tag, snapshot.Type, expectedRuntimeDigest, effectiveReader)
	snapshot.ConfigurationRevision = configurationRevision(snapshot, inbound.Tag, expectedRuntimeDigest)
	snapshot.Capability = ResolveNativeFallbackCapabilityV1(CapabilityInputV1{
		Runtime: identity, ShapeKnown: shapeKnown, InboundType: snapshot.Type, Listener: snapshot.Listener,
		TLS: snapshot.TLS, Transport: snapshot.Transport, Multiplex: snapshot.Multiplex,
		DefaultFallback: snapshot.DefaultFallback, ALPNFallbacks: snapshot.ALPNFallbacks,
	})
	snapshot.ReasonCodes = normalizedReasons(append(reasons, snapshot.Capability.ReasonCodes...))
	return snapshot
}

func populateTypedShapes(snapshot *InboundFallbackSnapshotV1, inbound model.Inbound) error {
	content, err := inbound.MarshalJSON()
	if err != nil {
		return err
	}
	ctx := sb.Context(context.Background(), coreregistry.InboundRegistry(), coreregistry.OutboundRegistry(),
		coreregistry.EndpointRegistry(), coreregistry.DNSTransportRegistry(), coreregistry.ServiceRegistry())
	var parsed option.Inbound
	if err = parsed.UnmarshalJSONContext(ctx, content); err != nil {
		return err
	}
	wrapper, ok := parsed.Options.(option.ListenOptionsWrapper)
	if !ok {
		return errShapeUnknown
	}
	listenOptions := wrapper.TakeListenOptions()
	if listenOptions.Listen == nil {
		snapshot.Listener.Bind = "*"
		snapshot.Listener.AddressFamily = "unknown"
	} else {
		address := listenOptions.Listen.Build(netip.IPv6Unspecified()).Unmap()
		snapshot.Listener.Bind = address.String()
		if address.Is4() {
			snapshot.Listener.AddressFamily = "ipv4"
		} else {
			snapshot.Listener.AddressFamily = "ipv6"
		}
	}
	snapshot.Listener.Port = listenOptions.ListenPort
	snapshot.Listener.Network = listenerNetworkForType(snapshot.Type)
	//lint:ignore SA1019 Pinned sing-box v1.13.14 still reads this compatibility field in common/listener/listener_tcp.go.
	snapshot.Listener.ProxyProtocol = listenOptions.ProxyProtocol

	switch typed := parsed.Options.(type) {
	case *option.RedirectInboundOptions:
		snapshot.Interception = InterceptionShapeV1{
			Candidate: true, Kind: "redirect", EffectiveNetworks: []string{"tcp"}, LinuxOnly: true,
			OriginalDestinationMechanism: "SO_ORIGINAL_DST", OriginalDestinationPreserved: true,
			SourcePreserved: true,
		}
	case *option.TProxyInboundOptions:
		snapshot.Interception = InterceptionShapeV1{
			Candidate: true, Kind: "tproxy", EffectiveNetworks: exactStrings(typed.Network.Build()), LinuxOnly: true,
			TransparentSocketRequired: true, OriginalDestinationMechanism: "IP_TRANSPARENT_RECVORIGDSTADDR",
			OriginalDestinationPreserved: true, SourcePreserved: true, PolicyRoutingRequired: true,
			BoundedUDPFlowState: containsString(typed.Network.Build(), "udp"),
		}
	case *option.SocksInboundOptions:
		snapshot.Authentication = AuthenticationShapeV1{Known: true, Expected: len(typed.Users) > 0, Count: len(typed.Users)}
		snapshot.LocalProxy = localProxyShape(snapshot.Type, typed.Users, nil, false)
		snapshot.UDPTransport = proxyAssociationShape(snapshot.Type)
	case *option.HTTPMixedInboundOptions:
		snapshot.Authentication = AuthenticationShapeV1{Known: true, Expected: len(typed.Users) > 0, Count: len(typed.Users)}
		snapshot.LocalProxy = localProxyShape(snapshot.Type, typed.Users, typed.TLS, typed.SetSystemProxy)
		if err := populateTLSShape(&snapshot.TLS, typed.TLS); err != nil {
			return err
		}
		snapshot.UDPTransport = proxyAssociationShape(snapshot.Type)
	case *option.DirectInboundOptions:
		snapshot.UDPTransport = networkTransportShape(typed.Network.Build(), "PLAIN_UDP", true)
	case *option.ShadowsocksInboundOptions:
		count := len(typed.Users)
		if strings.TrimSpace(typed.Password) != "" {
			count++
		}
		snapshot.Authentication = AuthenticationShapeV1{Known: true, Expected: count > 0, Count: count}
		snapshot.UDPTransport = networkTransportShape(typed.Network.Build(), "PLAIN_UDP", true)
	case *option.NaiveInboundOptions:
		snapshot.Authentication = AuthenticationShapeV1{Known: true, Expected: len(typed.Users) > 0, Count: len(typed.Users)}
		if err := populateTLSShape(&snapshot.TLS, typed.TLS); err != nil {
			return err
		}
		snapshot.UDPTransport = networkTransportShape(typed.Network.Build(), "QUIC_NATIVE", true)
		snapshot.UDPTransport.ProtocolOwnedMigration, snapshot.UDPTransport.ProtocolOwnedCID = true, true
	case *option.HysteriaInboundOptions:
		snapshot.Authentication = AuthenticationShapeV1{Known: true, Expected: len(typed.Users) > 0, Count: len(typed.Users)}
		if err := populateTLSShape(&snapshot.TLS, typed.TLS); err != nil {
			return err
		}
		snapshot.UDPTransport = networkTransportShape([]string{"udp"}, "QUIC_NATIVE", true)
		snapshot.UDPTransport.ProtocolOwnedMigration, snapshot.UDPTransport.ProtocolOwnedCID = true, true
	case *option.Hysteria2InboundOptions:
		snapshot.Authentication = AuthenticationShapeV1{Known: true, Expected: len(typed.Users) > 0, Count: len(typed.Users)}
		if err := populateTLSShape(&snapshot.TLS, typed.TLS); err != nil {
			return err
		}
		snapshot.UDPTransport = networkTransportShape([]string{"udp"}, "QUIC_NATIVE", true)
		snapshot.UDPTransport.ProtocolOwnedMigration, snapshot.UDPTransport.ProtocolOwnedCID = true, true
	case *option.TUICInboundOptions:
		snapshot.Authentication = AuthenticationShapeV1{Known: true, Expected: len(typed.Users) > 0, Count: len(typed.Users)}
		if err := populateTLSShape(&snapshot.TLS, typed.TLS); err != nil {
			return err
		}
		snapshot.UDPTransport = networkTransportShape([]string{"udp"}, "QUIC_NATIVE", true)
		snapshot.UDPTransport.ProtocolOwnedZeroRTT = typed.ZeroRTTHandshake
		snapshot.UDPTransport.ProtocolOwnedMigration, snapshot.UDPTransport.ProtocolOwnedCID = true, true
	case *option.VLESSInboundOptions:
		if err := populateCommonShapes(snapshot, typed.TLS, typed.Transport, typed.Multiplex); err != nil {
			return err
		}
		snapshot.Authentication = AuthenticationShapeV1{Known: true, Expected: len(typed.Users) > 0, Count: len(typed.Users)}
		if typed.Transport != nil && strings.EqualFold(strings.TrimSpace(typed.Transport.Type), "quic") {
			snapshot.UDPTransport = networkTransportShape([]string{"udp"}, "QUIC_V2RAY_TRANSPORT", true)
			snapshot.UDPTransport.ProtocolOwnedMigration, snapshot.UDPTransport.ProtocolOwnedCID = true, true
		}
	case *option.VMessInboundOptions:
		if err := populateCommonShapes(snapshot, typed.TLS, typed.Transport, typed.Multiplex); err != nil {
			return err
		}
		snapshot.Authentication = AuthenticationShapeV1{Known: true, Expected: len(typed.Users) > 0, Count: len(typed.Users)}
		if typed.Transport != nil && strings.EqualFold(strings.TrimSpace(typed.Transport.Type), "quic") {
			snapshot.UDPTransport = networkTransportShape([]string{"udp"}, "QUIC_V2RAY_TRANSPORT", true)
			snapshot.UDPTransport.ProtocolOwnedMigration, snapshot.UDPTransport.ProtocolOwnedCID = true, true
		}
	case *option.TrojanInboundOptions:
		if err := populateCommonShapes(snapshot, typed.TLS, typed.Transport, typed.Multiplex); err != nil {
			return err
		}
		snapshot.DefaultFallback = targetShape(typed.Fallback)
		if len(typed.FallbackForALPN) > 32 {
			return errShapeUnknown
		}
		keys := make([]string, 0, len(typed.FallbackForALPN))
		for key := range typed.FallbackForALPN {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		snapshot.ALPNFallbacks = make([]ALPNFallbackShapeV1, 0, len(keys))
		for _, key := range keys {
			if !safeALPN(key) {
				return errShapeUnknown
			}
			snapshot.ALPNFallbacks = append(snapshot.ALPNFallbacks, ALPNFallbackShapeV1{ALPN: key, Target: targetShape(typed.FallbackForALPN[key])})
		}
	default:
		if tlsWrapper, ok := parsed.Options.(option.InboundTLSOptionsWrapper); ok {
			if err := populateTLSShape(&snapshot.TLS, tlsWrapper.TakeInboundTLSOptions()); err != nil {
				return err
			}
		}
	}
	return nil
}

func localProxyShape(inboundType string, users []auth.User, tlsOptions *option.InboundTLSOptions, systemProxy bool) LocalProxyShapeV1 {
	inboundType = normalizeType(inboundType)
	if inboundType != "socks" && inboundType != "http" && inboundType != "mixed" {
		return LocalProxyShapeV1{}
	}
	shape := LocalProxyShapeV1{
		Candidate: true, Authentication: AuthenticationShapeV1{Known: true, Expected: len(users) > 0, Count: len(users)},
		SystemProxyKnown: true, SystemProxyEnabled: systemProxy, DependentUDPAssociation: inboundType == "socks" || inboundType == "mixed",
	}
	socks4Authenticated := false
	for _, user := range users {
		if strings.TrimSpace(user.Username) != "" && user.Password == "" {
			socks4Authenticated = true
			break
		}
	}
	shape.SOCKS4Authenticated = socks4Authenticated
	switch inboundType {
	case "socks":
		shape.Protocols = []string{"SOCKS5"}
		if len(users) == 0 || socks4Authenticated {
			shape.Protocols = append(shape.Protocols, "SOCKS4")
		}
	case "http":
		shape.Protocols = []string{"HTTP_FORWARD", "HTTP_CONNECT"}
	case "mixed":
		shape.Protocols = []string{"SOCKS5", "HTTP_FORWARD", "HTTP_CONNECT"}
		if len(users) == 0 || socks4Authenticated {
			shape.Protocols = append(shape.Protocols, "SOCKS4")
		}
	}
	if tlsOptions != nil {
		_ = populateTLSShape(&shape.TLS, tlsOptions)
	}
	return shape
}

func applyHydratedLocalProxyShape(snapshot *InboundFallbackSnapshotV1, content []byte) error {
	if snapshot == nil || !snapshot.LocalProxy.Candidate {
		return nil
	}
	ctx := sb.Context(context.Background(), coreregistry.InboundRegistry(), coreregistry.OutboundRegistry(),
		coreregistry.EndpointRegistry(), coreregistry.DNSTransportRegistry(), coreregistry.ServiceRegistry())
	var parsed option.Inbound
	if err := parsed.UnmarshalJSONContext(ctx, content); err != nil {
		return err
	}
	switch typed := parsed.Options.(type) {
	case *option.SocksInboundOptions:
		snapshot.LocalProxy = localProxyShape(snapshot.Type, typed.Users, nil, false)
	case *option.HTTPMixedInboundOptions:
		snapshot.LocalProxy = localProxyShape(snapshot.Type, typed.Users, typed.TLS, typed.SetSystemProxy)
	default:
		return errShapeUnknown
	}
	snapshot.Authentication = snapshot.LocalProxy.Authentication
	snapshot.Authentication.Revision = digestValue(struct {
		Schema         string
		Known, Present bool
		Count          int
	}{"solovey-ui/inbound-auth-shape/v2", snapshot.Authentication.Known, snapshot.Authentication.Expected, snapshot.Authentication.Count})
	finalizeLocalProxyShape(snapshot)
	return nil
}

func finalizeLocalProxyShape(snapshot *InboundFallbackSnapshotV1) {
	if snapshot == nil || !snapshot.LocalProxy.Candidate {
		return
	}
	shape := &snapshot.LocalProxy
	shape.Authentication = snapshot.Authentication
	if shape.Authentication.Expected && !shape.SOCKS4Authenticated {
		filtered := shape.Protocols[:0]
		for _, protocol := range shape.Protocols {
			if protocol != "SOCKS4" {
				filtered = append(filtered, protocol)
			}
		}
		shape.Protocols = filtered
	}
	shape.Protocols = exactStrings(shape.Protocols)
	shape.ProtocolRevision = digestValue(struct {
		Schema    string
		Type      string
		Protocols []string
	}{"solovey-ui/local-proxy-protocols/v1", snapshot.Type, shape.Protocols})
	shape.Authentication.Revision = snapshot.Authentication.Revision
	shape.TLSRevision = digestValue(struct {
		Schema string
		TLS    TLSShapeV1
	}{"solovey-ui/local-proxy-tls/v1", shape.TLS})
	shape.SystemProxyRevision = digestValue(struct {
		Schema  string
		Known   bool
		Enabled bool
	}{"solovey-ui/local-proxy-system-proxy/v1", shape.SystemProxyKnown, shape.SystemProxyEnabled})
}

func finalizeInterceptionShape(snapshot *InboundFallbackSnapshotV1) {
	if snapshot == nil || !snapshot.Interception.Candidate {
		return
	}
	shape := &snapshot.Interception
	shape.EffectiveNetworks = exactStrings(shape.EffectiveNetworks)
	shape.EffectiveNetworksRevision = digestValue(struct {
		Schema   string
		Networks []string
	}{"solovey-ui/interception-effective-networks/v1", shape.EffectiveNetworks})
	shape.SemanticRevision = digestValue(struct {
		Schema string
		Shape  InterceptionShapeV1
	}{"solovey-ui/core-interception-shape/v1", interceptionRevisionInput(*shape)})
	switch strings.Join(shape.EffectiveNetworks, ",") {
	case "tcp":
		snapshot.Listener.Network = "tcp"
	case "udp":
		snapshot.Listener.Network = "udp"
	case "tcp,udp":
		snapshot.Listener.Network = "tcp_udp"
	default:
		snapshot.Listener.Network = "unknown"
	}
}

func interceptionRevisionInput(value InterceptionShapeV1) InterceptionShapeV1 {
	value.SemanticRevision = ""
	return value
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func networkTransportShape(networks []string, class string, direct bool) UDPTransportShapeV2 {
	result := UDPTransportShapeV2{Class: class, DirectSocketActionable: direct}
	seen := map[string]bool{}
	for _, network := range networks {
		network = strings.ToLower(strings.TrimSpace(network))
		if (network == "tcp" || network == "udp") && !seen[network] {
			seen[network] = true
			result.EffectiveNetworks = append(result.EffectiveNetworks, network)
		}
	}
	sort.Strings(result.EffectiveNetworks)
	if !seen["udp"] {
		result.Class = "UNSUPPORTED"
		result.DirectSocketActionable = false
	} else if len(result.EffectiveNetworks) == 2 && class != "QUIC_NATIVE" && class != "QUIC_V2RAY_TRANSPORT" {
		result.Class = "TCP_UDP_DUAL"
	}
	return result
}

func proxyAssociationShape(inboundType string) UDPTransportShapeV2 {
	return UDPTransportShapeV2{EffectiveNetworks: []string{"tcp"}, Class: "PROXY_UDP_ASSOCIATION", DependentAssociation: true, DirectSocketActionable: false,
		ReasonCodes: []ReasonCode{ReasonProtocolOutOfScope}}
}

func finalizeUDPTransportShape(snapshot *InboundFallbackSnapshotV1, identity CoreRuntimeIdentityV1) {
	shape := &snapshot.UDPTransport
	if len(shape.EffectiveNetworks) == 0 {
		shape.EffectiveNetworks = []string{"tcp"}
		shape.Class = "UNSUPPORTED"
		shape.DirectSocketActionable = false
		shape.ReasonCodes = normalizedReasons(append(shape.ReasonCodes, ReasonInboundTypeUnsupported))
	}
	switch strings.Join(shape.EffectiveNetworks, ",") {
	case "tcp":
		snapshot.Listener.Network = "tcp"
	case "udp":
		snapshot.Listener.Network = "udp"
	case "tcp,udp":
		snapshot.Listener.Network = "tcp_udp"
	default:
		snapshot.Listener.Network = "unknown"
	}
	shape.EffectiveNetworkRevision = digestValue(struct {
		Schema   string
		Networks []string
	}{"solovey-ui/effective-inbound-networks/v2", shape.EffectiveNetworks})
	shape.TransportRevision = digestValue(struct {
		Schema, Type, Class string
		Transport           TransportShapeV1
	}{"solovey-ui/inbound-transport-semantics/v2", snapshot.Type, shape.Class, snapshot.Transport})
	shape.SocketIntentRevision = digestValue(struct {
		Schema   string
		Listener ListenerShapeV1
		Networks []string
	}{"solovey-ui/inbound-socket-intent/v2", snapshot.Listener, shape.EffectiveNetworks})
	shape.UDPTimeoutRevision = digestValue(struct{ Schema, Type, Runtime string }{"solovey-ui/inbound-udp-timeout-policy/v1", snapshot.Type, identity.IdentityRevision})
}

func populateCommonShapes(snapshot *InboundFallbackSnapshotV1, tlsOptions *option.InboundTLSOptions, transport *option.V2RayTransportOptions, multiplex *option.InboundMultiplexOptions) error {
	if err := populateTLSShape(&snapshot.TLS, tlsOptions); err != nil {
		return err
	}
	if transport != nil {
		snapshot.Transport = TransportShapeV1{Present: true, Type: strings.ToLower(strings.TrimSpace(transport.Type))}
	}
	if multiplex != nil {
		snapshot.Multiplex = MultiplexShapeV1{Present: true, Enabled: multiplex.Enabled}
	}
	return nil
}

func populateTLSShape(shape *TLSShapeV1, tlsOptions *option.InboundTLSOptions) error {
	if tlsOptions == nil {
		return nil
	}
	shape.Enabled = tlsOptions.Enabled
	if tlsOptions.ServerName != "" {
		shape.ServerNameDigest = digestValue(struct {
			Schema     string
			ServerName string
		}{"solovey-ui/inbound-tls-server-name/v1", strings.ToLower(tlsOptions.ServerName)})
	}
	if len(tlsOptions.ALPN) > 32 {
		return errShapeUnknown
	}
	for _, value := range []string(tlsOptions.ALPN) {
		if !safeALPN(value) {
			return errShapeUnknown
		}
	}
	shape.ALPN = exactStrings([]string(tlsOptions.ALPN))
	if tlsOptions.Reality != nil {
		shape.Reality = RealityShapeV1{
			Present: true, Enabled: tlsOptions.Reality.Enabled,
			Handshake: targetShape(&tlsOptions.Reality.Handshake.ServerOptions),
		}
	}
	return nil
}

func targetShape(options *option.ServerOptions) TargetShapeV1 {
	if options == nil {
		return TargetShapeV1{}
	}
	shape := TargetShapeV1{Present: true, Kind: "unknown"}
	server := strings.TrimSpace(options.Server)
	switch {
	case strings.HasPrefix(server, "/"), strings.HasPrefix(strings.ToLower(server), "unix:"), strings.Contains(server, `\`):
		shape.Kind = "unix"
	case server != "" && options.ServerPort != 0:
		shape.Kind = "tcp_host_port"
	}
	shape.Revision = digestValue(struct {
		Schema string
		Server string
		Port   uint16
	}{"solovey-ui/fallback-target-shape/v1", server, options.ServerPort})
	return shape
}

func effectiveInbound(expectedTag, expectedType, expectedOptionsDigest string, reader EffectiveInboundReader) EffectiveInboundV1 {
	result := EffectiveInboundV1{}
	if reader == nil {
		result.ReasonCodes = []ReasonCode{ReasonEffectiveRuntimeUnavailable}
		return result
	}
	runtimeAvailable, inboundType, inboundTag, present := reader.LookupInbound(expectedTag)
	optionsDigest := ""
	managerGeneration := uint64(0)
	if exact, ok := reader.(ExactEffectiveInboundReader); ok {
		runtimeAvailable, inboundType, inboundTag, optionsDigest, managerGeneration, present = exact.LookupInboundExact(expectedTag)
	}
	result.RuntimeAvailable = runtimeAvailable
	result.Present = present
	if !runtimeAvailable {
		result.ReasonCodes = []ReasonCode{ReasonEffectiveRuntimeUnavailable}
		return result
	}
	if !present {
		result.ReasonCodes = []ReasonCode{ReasonEffectiveInboundMissing}
		return result
	}
	result.Type = normalizeType(inboundType)
	result.Tag = safeTag(inboundTag)
	result.Revision = digestValue(struct {
		Schema            string
		Type              string
		Tag               string
		OptionsDigest     string
		ManagerGeneration uint64
	}{"solovey-ui/effective-inbound-presence/v2", result.Type, inboundTag, optionsDigest, managerGeneration})
	reasons := []ReasonCode(nil)
	result.ConfigurationProven = len(optionsDigest) == 64 && len(expectedOptionsDigest) == 64 &&
		optionsDigest == expectedOptionsDigest && managerGeneration != 0
	if !result.ConfigurationProven {
		reasons = append(reasons, ReasonEffectiveConfigurationUnproven)
	}
	if result.Type != expectedType || strings.TrimSpace(inboundTag) != strings.TrimSpace(expectedTag) {
		reasons = append(reasons, ReasonEffectiveInboundTypeMismatch)
	}
	result.ReasonCodes = normalizedReasons(reasons)
	return result
}

func configurationRevision(snapshot InboundFallbackSnapshotV1, rawTag, expectedRuntimeOptionsDigest string) string {
	return digestValue(struct {
		Schema               string
		InboundDatabaseID    uint
		ResourceID           string
		Tag                  string
		Type                 string
		InboundOptionsDigest string
		TLSRecordDatabaseID  uint
		TLSResourceID        string
		TLSOptionsDigest     string
		TLSReferenceCount    int64
		RuntimeOptionsDigest string
	}{
		"solovey-ui/inbound-fallback-configuration/v1", snapshot.InboundDatabaseID, snapshot.ResourceID,
		rawTag, snapshot.Type, snapshot.InboundOptionsDigest, snapshot.TLSRecordDatabaseID,
		snapshot.TLSResourceID, snapshot.TLSOptionsDigest, snapshot.TLSReferenceCount, expectedRuntimeOptionsDigest,
	})
}

func inboundResourceID(id uint, tag string) string {
	if id != 0 {
		return "core:inbound:" + strconv.FormatUint(uint64(id), 10)
	}
	if safe := safeTag(tag); safe != "" {
		return "core:inbound-tag:" + safe
	}
	return "core:inbound:unknown"
}

func listenerNetworkForType(inboundType string) string {
	switch normalizeType(inboundType) {
	case "hysteria", "hysteria2", "tuic":
		return "udp"
	case "mixed", "shadowsocks", "socks":
		return "tcp_udp"
	case "direct", "redirect", "tproxy", "tun":
		return "unknown"
	default:
		return "tcp"
	}
}

func normalizeType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 64 {
		return "unknown"
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return "unknown"
	}
	return value
}

func safeTag(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t") {
		return ""
	}
	return value
}

func containsReason(values []ReasonCode, target ReasonCode) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type shapeError struct{}

func (shapeError) Error() string { return "inbound shape is unavailable" }

var errShapeUnknown shapeError
