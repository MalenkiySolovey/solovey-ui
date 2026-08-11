package coreinboundcontrol

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/sagernet/sing-box/option"
	shadowsocks "github.com/sagernet/sing-shadowsocks2"
	M "github.com/sagernet/sing/common/metadata"
)

type PlainUDPProbeProviderV1 struct {
	control    *Service
	instance   string
	generation atomic.Uint64
}

func NewPlainUDPProbeProviderV1(control *Service) *PlainUDPProbeProviderV1 {
	seed := make([]byte, 16)
	if _, err := rand.Read(seed); err != nil {
		seed = []byte(strconv.FormatInt(time.Now().UTC().UnixNano(), 10))
	}
	return &PlainUDPProbeProviderV1{control: control, instance: "core-udp-probe:" + hostresources.Revision(seed)[:32]}
}
func (*PlainUDPProbeProviderV1) ProviderID() string { return "core-sing-box-plain-udp-probe-v1" }
func (p *PlainUDPProbeProviderV1) ProviderInstance() string {
	if p == nil {
		return ""
	}
	return p.instance
}

func (p *PlainUDPProbeProviderV1) Capability(ctx context.Context, target componenthealth.ProtocolProbeTargetV1) componenthealth.ProtocolProbeCapabilityV1 {
	value := componenthealth.ProtocolProbeCapabilityV1{ProviderID: p.ProviderID(), ProviderInstance: p.ProviderInstance(), ResourceID: target.ResourceID, EndpointID: target.EndpointID, ProtocolClass: target.ProtocolClass}
	if p == nil || p.control == nil {
		value.ReasonCodes = []string{"CORE_PROBE_UNAVAILABLE"}
		return componenthealth.FinalizeProtocolProbeCapabilityV1(value)
	}
	snapshot, err := p.snapshotForTarget(ctx, target)
	if err != nil || !strings.EqualFold(snapshot.Type, "shadowsocks") || !snapshotHasUDP(snapshot) ||
		(target.ProtocolClass != hostresources.TransportPlainUDP && target.ProtocolClass != hostresources.TransportTCPUDPDual) {
		value.ReasonCodes = []string{"PROTOCOL_PROBE_UNAVAILABLE"}
		return componenthealth.FinalizeProtocolProbeCapabilityV1(value)
	}
	value.Available = true
	return componenthealth.FinalizeProtocolProbeCapabilityV1(value)
}

func (p *PlainUDPProbeProviderV1) Probe(ctx context.Context, request componenthealth.ProtocolProbeRequestV1) (componenthealth.ProtocolProbeObservationV1, error) {
	capability := p.Capability(ctx, request.Target)
	if !capability.Available || capability.ProviderInstance != request.ProviderInstance {
		return componenthealth.ProtocolProbeObservationV1{}, errors.New("plain_udp_probe_unavailable")
	}
	started := time.Now().UTC().UnixNano()
	if started < request.NotBeforeUnixNano {
		return componenthealth.ProtocolProbeObservationV1{}, errors.New("plain_udp_probe_boundary_not_reached")
	}
	snapshot, err := p.snapshotForTarget(ctx, request.Target)
	if err != nil {
		return componenthealth.ProtocolProbeObservationV1{}, err
	}
	var inbound model.Inbound
	if err = p.control.db.WithContext(ctx).First(&inbound, snapshot.InboundDatabaseID).Error; err != nil {
		return componenthealth.ProtocolProbeObservationV1{}, errors.New("plain_udp_probe_configuration_unavailable")
	}
	content, err := inbound.MarshalJSON()
	if err != nil {
		return componenthealth.ProtocolProbeObservationV1{}, errors.New("plain_udp_probe_configuration_unavailable")
	}
	if p.control.mutation.Hydrator == nil {
		return componenthealth.ProtocolProbeObservationV1{}, errors.New("plain_udp_probe_credentials_unavailable")
	}
	content, err = p.control.mutation.Hydrator.HydrateInbound(ctx, p.control.db.WithContext(ctx), &inbound, content)
	if err != nil {
		return componenthealth.ProtocolProbeObservationV1{}, errors.New("plain_udp_probe_credentials_unavailable")
	}
	var options option.ShadowsocksInboundOptions
	if json.Unmarshal(content, &options) != nil || strings.TrimSpace(options.Method) == "" {
		return componenthealth.ProtocolProbeObservationV1{}, errors.New("plain_udp_probe_configuration_invalid")
	}
	password := options.Password
	if password == "" && len(options.Users) > 0 {
		password = options.Users[0].Password
	}
	if password == "" {
		return componenthealth.ProtocolProbeObservationV1{}, errors.New("plain_udp_probe_credentials_unavailable")
	}
	method, err := shadowsocks.CreateMethod(ctx, options.Method, shadowsocks.MethodOptions{Password: password})
	if err != nil {
		return componenthealth.ProtocolProbeObservationV1{}, errors.New("plain_udp_probe_method_unavailable")
	}
	network, loopback := "udp4", "127.0.0.1"
	if request.Target.AddressFamily == hostresources.AddressFamilyIPv6 {
		network, loopback = "udp6", "::1"
	}
	echo, err := net.ListenUDP(network, &net.UDPAddr{IP: net.ParseIP(loopback), Port: 0})
	if err != nil {
		return componenthealth.ProtocolProbeObservationV1{}, errors.New("plain_udp_probe_fixture_unavailable")
	}
	defer echo.Close()
	echoDone := make(chan error, 1)
	go func() {
		buffer := make([]byte, 256)
		_ = echo.SetReadDeadline(time.Now().Add(componenthealth.MaxProtocolProbeDuration))
		n, peer, readErr := echo.ReadFromUDP(buffer)
		if readErr == nil {
			_, readErr = echo.WriteToUDP(buffer[:n], peer)
		}
		echoDone <- readErr
	}()
	serverHost := request.Target.ConfiguredBind
	if serverHost == "0.0.0.0" {
		serverHost = "127.0.0.1"
	}
	if serverHost == "::" {
		serverHost = "::1"
	}
	serverAddr, err := net.ResolveUDPAddr(network, net.JoinHostPort(serverHost, strconv.Itoa(int(request.Target.Port))))
	if err != nil {
		return componenthealth.ProtocolProbeObservationV1{}, errors.New("plain_udp_probe_target_invalid")
	}
	outConn, err := net.DialUDP(network, nil, serverAddr)
	if err != nil {
		return componenthealth.ProtocolProbeObservationV1{}, errors.New("plain_udp_probe_target_unreachable")
	}
	packetConn := method.DialPacketConn(outConn)
	defer packetConn.Close()
	_ = packetConn.SetDeadline(time.Now().Add(componenthealth.MaxProtocolProbeDuration))
	payload := []byte(request.ChallengeRevision)
	destination := M.SocksaddrFromNet(echo.LocalAddr())
	if _, err = packetConn.WriteTo(payload, destination); err != nil {
		return componenthealth.ProtocolProbeObservationV1{}, errors.New("plain_udp_probe_request_failed")
	}
	response := make([]byte, 256)
	n, responder, err := packetConn.ReadFrom(response)
	if err != nil {
		return componenthealth.ProtocolProbeObservationV1{}, errors.New("plain_udp_probe_response_failed")
	}
	if !bytes.Equal(response[:n], payload) || M.SocksaddrFromNet(responder).Unwrap() != destination.Unwrap() {
		return componenthealth.ProtocolProbeObservationV1{}, errors.New("plain_udp_probe_wrong_responder")
	}
	select {
	case echoErr := <-echoDone:
		if echoErr != nil {
			return componenthealth.ProtocolProbeObservationV1{}, errors.New("plain_udp_probe_fixture_failed")
		}
	default:
	}
	after, err := p.snapshotForTarget(ctx, request.Target)
	if err != nil || after.ConfigurationRevision != snapshot.ConfigurationRevision || after.Effective.Revision != snapshot.Effective.Revision {
		return componenthealth.ProtocolProbeObservationV1{}, errors.New("plain_udp_probe_runtime_drift")
	}
	generation := p.nextGeneration(request.MinimumGeneration)
	probeID := hostresources.Revision(struct {
		Challenge  string
		Generation uint64
		Started    int64
	}{request.ChallengeRevision, generation, started})
	completed := time.Now().UTC().UnixNano()
	value := componenthealth.ProtocolProbeObservationV1{ProviderID: p.ProviderID(), ProviderInstance: p.ProviderInstance(), ResourceID: request.Target.ResourceID, EndpointID: request.Target.EndpointID, ProtocolClass: request.Target.ProtocolClass,
		RuntimeRevision: request.Target.RuntimeRevision, CapabilityRevision: request.Target.CapabilityRevision, ConfigurationRevision: request.Target.ConfigurationRevision, SocketRevision: request.Target.SocketRevision,
		ContributionRevision: request.ContributionRevision, CompositionRevision: request.CompositionRevision, ManagedPlanRevision: request.ManagedPlanRevision, ChallengeRevision: request.ChallengeRevision,
		Generation: generation, ProbeID: probeID, StartedUnixNano: started, CompletedUnixNano: completed, ExpiresUnixNano: completed + int64(time.Minute), Passed: true, RequestResponse: true, ExactTarget: true,
		ResponderRevision: hostresources.Revision(struct{ Challenge, Probe string }{request.ChallengeRevision, probeID})}
	return componenthealth.FinalizeProtocolProbeObservationV1(value), nil
}

func (p *PlainUDPProbeProviderV1) nextGeneration(minimum uint64) uint64 {
	for {
		current := p.generation.Load()
		next := current + 1
		if next < minimum {
			next = minimum
		}
		if p.generation.CompareAndSwap(current, next) {
			return next
		}
	}
}
func (p *PlainUDPProbeProviderV1) snapshotForTarget(ctx context.Context, target componenthealth.ProtocolProbeTargetV1) (InboundFallbackSnapshotV1, error) {
	prefix := "core:inbound:"
	if !strings.HasPrefix(target.ResourceID, prefix) {
		return InboundFallbackSnapshotV1{}, errors.New("plain_udp_probe_resource_invalid")
	}
	id64, err := strconv.ParseUint(strings.TrimPrefix(target.ResourceID, prefix), 10, 64)
	if err != nil || id64 == 0 {
		return InboundFallbackSnapshotV1{}, errors.New("plain_udp_probe_resource_invalid")
	}
	snapshot, err := p.control.Snapshot(ctx, uint(id64))
	if err != nil {
		return InboundFallbackSnapshotV1{}, err
	}
	if snapshot.ResourceID != target.ResourceID || snapshot.ConfigurationRevision != target.ConfigurationRevision || snapshot.Effective.Revision != target.RuntimeRevision ||
		snapshot.Listener.Port != target.Port || snapshot.Listener.Bind != target.ConfiguredBind || snapshot.Listener.AddressFamily != string(target.AddressFamily) {
		return InboundFallbackSnapshotV1{}, errors.New("plain_udp_probe_revision_drift")
	}
	return snapshot, nil
}
func snapshotHasUDP(value InboundFallbackSnapshotV1) bool {
	for _, network := range value.UDPTransport.EffectiveNetworks {
		if network == "udp" {
			return true
		}
	}
	return false
}
