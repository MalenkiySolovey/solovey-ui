package coreinboundcontrol

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	coreregistry "github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	sb "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/auth"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	singhttp "github.com/sagernet/sing/protocol/http"
	"github.com/sagernet/sing/protocol/socks"
)

type LocalProxyProbeProviderV1 struct {
	control    *Service
	instance   string
	generation atomic.Uint64
}

func NewLocalProxyProbeProviderV1(control *Service) *LocalProxyProbeProviderV1 {
	seed := make([]byte, 16)
	if _, err := rand.Read(seed); err != nil {
		seed = []byte(strconv.FormatInt(time.Now().UTC().UnixNano(), 10))
	}
	return &LocalProxyProbeProviderV1{
		control: control, instance: "core-local-proxy-probe:" + hostresources.Revision(seed)[:32],
	}
}

func (*LocalProxyProbeProviderV1) ProviderID() string { return "core-sing-box-local-proxy-probe-v1" }
func (p *LocalProxyProbeProviderV1) ProviderInstance() string {
	if p == nil {
		return ""
	}
	return p.instance
}

func (p *LocalProxyProbeProviderV1) Capability(ctx context.Context, target componenthealth.LocalProxyProbeTargetV1) componenthealth.LocalProxyProbeCapabilityV1 {
	value := componenthealth.LocalProxyProbeCapabilityV1{
		ProviderID: p.ProviderID(), ProviderInstance: p.ProviderInstance(), ResourceID: target.ResourceID,
		EndpointID: target.EndpointID, Protocol: target.Protocol,
	}
	if p == nil || p.control == nil || p.control.db == nil {
		value.ReasonCodes = []string{"CORE_LOCAL_PROXY_PROBE_UNAVAILABLE"}
		return componenthealth.FinalizeLocalProxyProbeCapabilityV1(value)
	}
	snapshot, err := p.snapshotForTarget(ctx, target)
	if err != nil || !containsLocalProxyProtocol(snapshot.LocalProxy.Protocols, target.Protocol) ||
		p.validateGuardLease(ctx, target) != nil {
		value.ReasonCodes = []string{"LOCAL_PROXY_PROBE_UNAVAILABLE"}
		return componenthealth.FinalizeLocalProxyProbeCapabilityV1(value)
	}
	value.Available = true
	return componenthealth.FinalizeLocalProxyProbeCapabilityV1(value)
}

func (p *LocalProxyProbeProviderV1) Probe(ctx context.Context, request componenthealth.LocalProxyProbeRequestV1) (componenthealth.LocalProxyProbeObservationV1, error) {
	capability := p.Capability(ctx, request.Target)
	if !capability.Available || capability.ProviderInstance != request.ProviderInstance {
		return componenthealth.LocalProxyProbeObservationV1{}, errors.New("local_proxy_probe_unavailable")
	}
	started := time.Now().UTC().UnixNano()
	if started < request.NotBeforeUnixNano {
		return componenthealth.LocalProxyProbeObservationV1{}, errors.New("local_proxy_probe_boundary_not_reached")
	}
	snapshot, err := p.snapshotForTarget(ctx, request.Target)
	if err != nil {
		return componenthealth.LocalProxyProbeObservationV1{}, err
	}
	config, err := p.localProxyProbeConfig(ctx, snapshot)
	if err != nil {
		return componenthealth.LocalProxyProbeObservationV1{}, errors.New("local_proxy_probe_configuration_unavailable")
	}
	server, err := localProxyServerAddress(snapshot.Listener)
	if err != nil {
		return componenthealth.LocalProxyProbeObservationV1{}, errors.New("local_proxy_probe_listener_invalid")
	}
	dialer := localProxyNetworkDialer{server: server, tlsEnabled: config.tls != nil && config.tls.Enabled}
	if config.tls != nil {
		dialer.serverName = strings.TrimSpace(config.tls.ServerName)
	}
	authRequired := len(config.users) > 0
	positive := false
	missingDenied, invalidDenied := !authRequired, !authRequired
	sinkRevision := ""
	switch request.Target.Protocol {
	case hostresources.LocalProxyProtocolSOCKS4:
		user, ok := socks4ProbeUser(config.users)
		if authRequired && !ok {
			return componenthealth.LocalProxyProbeObservationV1{}, errors.New("local_proxy_probe_credentials_unavailable")
		}
		if authRequired {
			missingDenied, err = negativeSOCKSProbe(ctx, dialer, server, socks.Version4, "", "")
			if err != nil {
				return componenthealth.LocalProxyProbeObservationV1{}, err
			}
			invalidDenied, err = negativeSOCKSProbe(ctx, dialer, server, socks.Version4, invalidProxyCredential(), "")
			if err != nil {
				return componenthealth.LocalProxyProbeObservationV1{}, err
			}
		}
		positive, sinkRevision, err = positiveSOCKSProbe(ctx, dialer, server, socks.Version4, user.Username, "", request.ChallengeRevision)
	case hostresources.LocalProxyProtocolSOCKS5:
		user := firstProxyUser(config.users)
		if authRequired {
			missingDenied, err = negativeSOCKSProbe(ctx, dialer, server, socks.Version5, "", "")
			if err == nil {
				invalidDenied, err = negativeSOCKSProbe(ctx, dialer, server, socks.Version5, invalidProxyCredential(), invalidProxyCredential())
			}
			if err != nil {
				return componenthealth.LocalProxyProbeObservationV1{}, err
			}
		}
		positive, sinkRevision, err = positiveSOCKSProbe(ctx, dialer, server, socks.Version5, user.Username, user.Password, request.ChallengeRevision)
	case hostresources.LocalProxyProtocolHTTPConnect:
		user := firstProxyUser(config.users)
		if authRequired {
			missingDenied, err = negativeHTTPConnectProbe(ctx, dialer, server, auth.User{})
			if err == nil {
				invalidDenied, err = negativeHTTPConnectProbe(ctx, dialer, server, auth.User{Username: invalidProxyCredential(), Password: invalidProxyCredential()})
			}
			if err != nil {
				return componenthealth.LocalProxyProbeObservationV1{}, err
			}
		}
		positive, sinkRevision, err = positiveHTTPConnectProbe(ctx, dialer, server, user, request.ChallengeRevision)
	case hostresources.LocalProxyProtocolHTTPForward:
		user := firstProxyUser(config.users)
		if authRequired {
			missingDenied, err = negativeHTTPForwardProbe(ctx, dialer, server, auth.User{})
			if err == nil {
				invalidDenied, err = negativeHTTPForwardProbe(ctx, dialer, server, auth.User{Username: invalidProxyCredential(), Password: invalidProxyCredential()})
			}
			if err != nil {
				return componenthealth.LocalProxyProbeObservationV1{}, err
			}
			select {
			case <-ctx.Done():
				return componenthealth.LocalProxyProbeObservationV1{}, ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
		positive, sinkRevision, err = positiveHTTPForwardProbe(ctx, dialer, server, user, request.ChallengeRevision)
		if err != nil {
			// A proxy may close a completed forward response at the body
			// boundary. Permit one bounded fresh transaction to distinguish
			// that transport race from a semantic failure.
			positive, sinkRevision, err = positiveHTTPForwardProbe(ctx, dialer, server, user, request.ChallengeRevision)
		}
	default:
		return componenthealth.LocalProxyProbeObservationV1{}, errors.New("local_proxy_probe_protocol_unavailable")
	}
	if err != nil {
		return componenthealth.LocalProxyProbeObservationV1{}, errors.New("local_proxy_probe_transaction_error")
	}
	if !positive {
		return componenthealth.LocalProxyProbeObservationV1{}, errors.New("local_proxy_probe_positive_transaction_failed")
	}
	if authRequired && !missingDenied {
		return componenthealth.LocalProxyProbeObservationV1{}, errors.New("local_proxy_probe_missing_authentication_denial_failed")
	}
	if authRequired && !invalidDenied {
		return componenthealth.LocalProxyProbeObservationV1{}, errors.New("local_proxy_probe_invalid_authentication_denial_failed")
	}
	after, err := p.snapshotForTarget(ctx, request.Target)
	if err != nil || after.ConfigurationRevision != snapshot.ConfigurationRevision ||
		after.Effective.Revision != snapshot.Effective.Revision || p.validateGuardLease(ctx, request.Target) != nil {
		return componenthealth.LocalProxyProbeObservationV1{}, errors.New("local_proxy_probe_runtime_drift")
	}
	generation := p.nextGeneration(request.MinimumGeneration)
	probeID := hostresources.Revision(struct {
		Challenge, Protocol, Sink string
		Generation                uint64
		Started                   int64
	}{request.ChallengeRevision, string(request.Target.Protocol), sinkRevision, generation, started})
	completed := time.Now().UTC().UnixNano()
	target := request.Target
	value := componenthealth.LocalProxyProbeObservationV1{
		ProviderID: p.ProviderID(), ProviderInstance: p.ProviderInstance(), ResourceID: target.ResourceID,
		EndpointID: target.EndpointID, Protocol: target.Protocol, ConfigurationRevision: target.ConfigurationRevision,
		RuntimeRevision: target.RuntimeRevision, FactRevision: target.FactRevision,
		ListenerObservationRevision: target.ListenerObservationRevision, AuthenticationRevision: target.AuthenticationRevision,
		TLSRevision: target.TLSRevision, SystemProxyRevision: target.SystemProxyRevision,
		LeaseID: target.LeaseID, LeaseRevision: target.LeaseRevision, LeaseState: target.LeaseState,
		OperationID: target.OperationID, OperationRevision: target.OperationRevision,
		PlanRevision: target.PlanRevision, MarkerRevision: target.MarkerRevision,
		ChallengeRevision: request.ChallengeRevision, Generation: generation, ProbeID: probeID,
		StartedUnixNano: started, CompletedUnixNano: completed, ExpiresUnixNano: completed + int64(time.Minute),
		Passed: true, PositiveTransaction: true, MissingAuthenticationDenied: missingDenied,
		InvalidAuthenticationDenied: invalidDenied, ExactTarget: true, ExactSink: true, SinkRevision: sinkRevision,
	}
	value.ResponderRevision = hostresources.Revision(struct{ Challenge, Probe, Sink string }{
		value.ChallengeRevision, value.ProbeID, value.SinkRevision,
	})
	return componenthealth.FinalizeLocalProxyProbeObservationV1(value), nil
}

type localProxyProbeConfigV1 struct {
	users []auth.User
	tls   *option.InboundTLSOptions
}

func (p *LocalProxyProbeProviderV1) localProxyProbeConfig(ctx context.Context, snapshot InboundFallbackSnapshotV1) (localProxyProbeConfigV1, error) {
	var inbound model.Inbound
	if err := p.control.db.WithContext(ctx).Preload("Tls").First(&inbound, snapshot.InboundDatabaseID).Error; err != nil {
		return localProxyProbeConfigV1{}, err
	}
	content, err := p.control.hydratedInboundContent(ctx, &inbound)
	if err != nil {
		return localProxyProbeConfigV1{}, err
	}
	parseContext := sb.Context(context.Background(), coreregistry.InboundRegistry(), coreregistry.OutboundRegistry(),
		coreregistry.EndpointRegistry(), coreregistry.DNSTransportRegistry(), coreregistry.ServiceRegistry())
	var parsed option.Inbound
	if err := parsed.UnmarshalJSONContext(parseContext, content); err != nil {
		return localProxyProbeConfigV1{}, err
	}
	switch typed := parsed.Options.(type) {
	case *option.SocksInboundOptions:
		return localProxyProbeConfigV1{users: append([]auth.User(nil), typed.Users...)}, nil
	case *option.HTTPMixedInboundOptions:
		return localProxyProbeConfigV1{users: append([]auth.User(nil), typed.Users...), tls: typed.TLS}, nil
	default:
		return localProxyProbeConfigV1{}, errors.New("local_proxy_probe_configuration_invalid")
	}
}

func (p *LocalProxyProbeProviderV1) snapshotForTarget(ctx context.Context, target componenthealth.LocalProxyProbeTargetV1) (InboundFallbackSnapshotV1, error) {
	const prefix = "core:inbound:"
	if !strings.HasPrefix(target.ResourceID, prefix) {
		return InboundFallbackSnapshotV1{}, errors.New("local_proxy_probe_resource_invalid")
	}
	id64, err := strconv.ParseUint(strings.TrimPrefix(target.ResourceID, prefix), 10, 64)
	if err != nil || id64 == 0 {
		return InboundFallbackSnapshotV1{}, errors.New("local_proxy_probe_resource_invalid")
	}
	snapshot, err := p.control.Snapshot(ctx, uint(id64))
	if err != nil {
		return InboundFallbackSnapshotV1{}, err
	}
	if snapshot.ResourceID != target.ResourceID || snapshot.ConfigurationRevision != target.ConfigurationRevision ||
		snapshot.Effective.Revision != target.RuntimeRevision || !snapshot.LocalProxy.Candidate ||
		!snapshot.Effective.ConfigurationProven || !snapshot.Effective.Present {
		return InboundFallbackSnapshotV1{}, errors.New("local_proxy_probe_revision_drift")
	}
	return snapshot, nil
}

func (p *LocalProxyProbeProviderV1) validateGuardLease(ctx context.Context, target componenthealth.LocalProxyProbeTargetV1) error {
	var row model.InboundEndpointLease
	if err := p.control.db.WithContext(ctx).Where("lease_id = ?", target.LeaseID).First(&row).Error; err != nil {
		return err
	}
	if row.ProviderID != target.ProviderID || row.ResourceID != target.ResourceID || row.EndpointID != target.EndpointID ||
		row.LeaseRevision != target.LeaseRevision || row.State != string(target.LeaseState) ||
		row.ExpiresAtUnix <= time.Now().UTC().Unix() ||
		(row.State != string(hostresources.EndpointLeaseMutationPending) && row.State != string(hostresources.EndpointLeaseActive)) {
		return errors.New("local_proxy_probe_guard_lease_drift")
	}
	return nil
}

func (p *LocalProxyProbeProviderV1) nextGeneration(minimum uint64) uint64 {
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

type localProxyNetworkDialer struct {
	server     M.Socksaddr
	tlsEnabled bool
	serverName string
}

func (d localProxyNetworkDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	if destination.String() != d.server.String() || network != "tcp" {
		return nil, errors.New("local_proxy_probe_dial_target_rejected")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, network, destination.String())
	if err != nil || !d.tlsEnabled {
		return conn, err
	}
	// TLS identity is not used as the authority boundary here: the provider
	// lease plus exact listener-owner fact bind this local socket. The probe
	// still proves the configured TLS transport completes before proxy bytes.
	tlsConn := tls.Client(conn, &tls.Config{ //nolint:gosec
		MinVersion: tls.VersionTLS12, ServerName: d.serverName, InsecureSkipVerify: true,
	})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return tlsConn, nil
}

func (localProxyNetworkDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, errors.New("local_proxy_probe_udp_dial_rejected")
}

func localProxyServerAddress(listener ListenerShapeV1) (M.Socksaddr, error) {
	host := strings.TrimSpace(listener.Bind)
	if host == "" || host == "*" || host == "0.0.0.0" || host == "::" || listener.Port == 0 {
		return M.Socksaddr{}, errors.New("local_proxy_probe_listener_invalid")
	}
	address := M.ParseSocksaddrHostPort(host, listener.Port)
	if !address.IsValid() {
		return M.Socksaddr{}, errors.New("local_proxy_probe_listener_invalid")
	}
	return address, nil
}

func positiveSOCKSProbe(ctx context.Context, dialer N.Dialer, server M.Socksaddr, version socks.Version, username, password, challenge string) (bool, string, error) {
	listener, sink, err := newLocalProxyTCPSink(server, challenge)
	if err != nil {
		return false, "", err
	}
	defer listener.Close()
	client := socks.NewClient(dialer, server, version, username, password)
	conn, err := client.DialContext(ctx, "tcp", sink)
	if err != nil {
		return false, "", err
	}
	defer conn.Close()
	if err := roundTripChallenge(conn, challenge); err != nil {
		return false, "", err
	}
	return true, sinkRevision(sink, challenge), nil
}

func negativeSOCKSProbe(ctx context.Context, dialer N.Dialer, server M.Socksaddr, version socks.Version, username, password string) (bool, error) {
	listener, sink, err := newDenialSink(server)
	if err != nil {
		return false, err
	}
	defer listener.Close()
	client := socks.NewClient(dialer, server, version, username, password)
	conn, dialErr := client.DialContext(ctx, "tcp", sink)
	if conn != nil {
		_ = conn.Close()
	}
	reached := denialSinkReached(listener)
	return dialErr != nil && !reached, nil
}

func positiveHTTPConnectProbe(ctx context.Context, dialer N.Dialer, server M.Socksaddr, user auth.User, challenge string) (bool, string, error) {
	listener, sink, err := newLocalProxyTCPSink(server, challenge)
	if err != nil {
		return false, "", err
	}
	defer listener.Close()
	client := singhttp.NewClient(singhttp.Options{Dialer: dialer, Server: server, Username: user.Username, Password: user.Password})
	conn, err := client.DialContext(ctx, "tcp", sink)
	if err != nil {
		return false, "", err
	}
	defer conn.Close()
	if err := roundTripChallenge(conn, challenge); err != nil {
		return false, "", err
	}
	return true, sinkRevision(sink, challenge), nil
}

func negativeHTTPConnectProbe(ctx context.Context, dialer N.Dialer, server M.Socksaddr, user auth.User) (bool, error) {
	listener, sink, err := newDenialSink(server)
	if err != nil {
		return false, err
	}
	defer listener.Close()
	client := singhttp.NewClient(singhttp.Options{Dialer: dialer, Server: server, Username: user.Username, Password: user.Password})
	conn, dialErr := client.DialContext(ctx, "tcp", sink)
	if conn != nil {
		_ = conn.Close()
	}
	return dialErr != nil && !denialSinkReached(listener), nil
}

func positiveHTTPForwardProbe(ctx context.Context, dialer N.Dialer, server M.Socksaddr, user auth.User, challenge string) (bool, string, error) {
	listener, sink, err := newHTTPChallengeSink(server, challenge)
	if err != nil {
		return false, "", err
	}
	defer listener.Close()
	status, body, err := httpForwardRequest(ctx, dialer, server, sink, user, challenge)
	exactResponse := status == http.StatusOK && body == challenge
	if exactResponse {
		// The complete challenge body is authoritative even when Windows
		// reports a connection reset at the response boundary.
		err = nil
	}
	return exactResponse, sinkRevision(sink, challenge), err
}

func negativeHTTPForwardProbe(ctx context.Context, dialer N.Dialer, server M.Socksaddr, user auth.User) (bool, error) {
	listener, sink, err := newDenialSink(server)
	if err != nil {
		return false, err
	}
	defer listener.Close()
	status, _, requestErr := httpForwardRequest(ctx, dialer, server, sink, user, "denied")
	reached := denialSinkReached(listener)
	// The 407 response header is the authentication proof. Some proxy
	// implementations close the response body immediately, which may surface
	// as EOF after the already-valid header has been parsed.
	if status != http.StatusProxyAuthRequired && requestErr == nil {
		return false, errors.New("local_proxy_probe_http_forward_authentication_status_invalid")
	}
	if reached {
		return false, errors.New("local_proxy_probe_http_forward_denial_sink_reached")
	}
	return true, nil
}

func httpForwardRequest(ctx context.Context, dialer N.Dialer, server, sink M.Socksaddr, user auth.User, challenge string) (int, string, error) {
	conn, err := dialer.DialContext(ctx, "tcp", server)
	if err != nil {
		return 0, "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(componenthealth.MaxLocalProxyProbeDurationV1))
	requestURL := &url.URL{Scheme: "http", Host: sink.String(), Path: "/probe/" + challenge}
	request := &http.Request{Method: http.MethodGet, URL: requestURL, Host: sink.String(), Header: make(http.Header)}
	if user.Username != "" {
		credential := base64.StdEncoding.EncodeToString([]byte(user.Username + ":" + user.Password))
		request.Header.Set("Proxy-Authorization", "Basic "+credential)
	}
	if err := request.WriteProxy(conn); err != nil {
		return 0, "", err
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), request)
	if err != nil {
		return 0, "", err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1024))
	return response.StatusCode, string(body), err
}

func newLocalProxyTCPSink(server M.Socksaddr, challenge string) (*net.TCPListener, M.Socksaddr, error) {
	listener, sink, err := listenLocalProxySink(server)
	if err != nil {
		return nil, M.Socksaddr{}, err
	}
	go func() {
		_ = listener.SetDeadline(time.Now().Add(componenthealth.MaxLocalProxyProbeDurationV1))
		conn, acceptErr := listener.AcceptTCP()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(componenthealth.MaxLocalProxyProbeDurationV1))
		buffer := make([]byte, len(challenge))
		if _, readErr := io.ReadFull(conn, buffer); readErr == nil && string(buffer) == challenge {
			_, _ = conn.Write(buffer)
		}
	}()
	return listener, sink, nil
}

func newHTTPChallengeSink(server M.Socksaddr, challenge string) (*net.TCPListener, M.Socksaddr, error) {
	listener, sink, err := listenLocalProxySink(server)
	if err != nil {
		return nil, M.Socksaddr{}, err
	}
	go func() {
		_ = listener.SetDeadline(time.Now().Add(componenthealth.MaxLocalProxyProbeDurationV1))
		conn, acceptErr := listener.AcceptTCP()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(componenthealth.MaxLocalProxyProbeDurationV1))
		request, readErr := http.ReadRequest(bufio.NewReader(conn))
		if readErr != nil {
			return
		}
		_ = request.Body.Close()
		if request.URL.Path != "/probe/"+challenge {
			return
		}
		response := &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", ProtoMajor: 1, ProtoMinor: 1,
			Header: http.Header{"Content-Type": []string{"text/plain"}, "Content-Length": []string{strconv.Itoa(len(challenge))}},
			Body:   io.NopCloser(strings.NewReader(challenge)),
		}
		_ = response.Write(conn)
	}()
	return listener, sink, nil
}

func newDenialSink(server M.Socksaddr) (*net.TCPListener, M.Socksaddr, error) {
	return listenLocalProxySink(server)
}

func listenLocalProxySink(server M.Socksaddr) (*net.TCPListener, M.Socksaddr, error) {
	network, host := "tcp4", "127.0.0.1"
	if server.Addr.Is6() {
		network, host = "tcp6", "::1"
	}
	listener, err := net.ListenTCP(network, &net.TCPAddr{IP: net.ParseIP(host), Port: 0})
	if err != nil {
		return nil, M.Socksaddr{}, errors.New("local_proxy_probe_sink_unavailable")
	}
	return listener, M.SocksaddrFromNet(listener.Addr()), nil
}

func denialSinkReached(listener *net.TCPListener) bool {
	if listener == nil {
		return true
	}
	_ = listener.SetDeadline(time.Now().Add(75 * time.Millisecond))
	conn, err := listener.AcceptTCP()
	if conn != nil {
		_ = conn.Close()
	}
	return err == nil
}

func roundTripChallenge(conn net.Conn, challenge string) error {
	_ = conn.SetDeadline(time.Now().Add(componenthealth.MaxLocalProxyProbeDurationV1))
	if _, err := conn.Write([]byte(challenge)); err != nil {
		return err
	}
	response := make([]byte, len(challenge))
	if _, err := io.ReadFull(conn, response); err != nil || !bytes.Equal(response, []byte(challenge)) {
		return errors.New("local_proxy_probe_wrong_responder")
	}
	return nil
}

func sinkRevision(sink M.Socksaddr, challenge string) string {
	return hostresources.Revision(struct{ Schema, Sink, Challenge string }{
		"solovey-ui/local-proxy-probe-sink/v1", sink.String(), challenge,
	})
}

func firstProxyUser(users []auth.User) auth.User {
	if len(users) == 0 {
		return auth.User{}
	}
	return users[0]
}

func socks4ProbeUser(users []auth.User) (auth.User, bool) {
	if len(users) == 0 {
		return auth.User{}, true
	}
	for _, user := range users {
		if user.Username != "" && user.Password == "" {
			return user, true
		}
	}
	return auth.User{}, false
}

func invalidProxyCredential() string {
	seed := make([]byte, 32)
	_, _ = rand.Read(seed)
	return "invalid-" + hostresources.Revision(seed)[:24]
}

func containsLocalProxyProtocol(protocols []string, wanted hostresources.LocalProxyProtocolV1) bool {
	for _, protocol := range protocols {
		if protocol == string(wanted) {
			return true
		}
	}
	return false
}

var (
	_ componenthealth.LocalProxyProbeProviderV1 = (*LocalProxyProbeProviderV1)(nil)
	_ N.Dialer                                  = localProxyNetworkDialer{}
)
