package coreinboundcontrol

import (
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	corebox "github.com/MalenkiySolovey/solovey-ui/core/box"
	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	sb "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
)

func TestLocalProxyProbePerformsAuthenticatedSOCKSHTTPAndMixedTransactions(t *testing.T) {
	tests := []struct {
		name        string
		inboundType string
		password    string
		protocols   []hostresources.LocalProxyProtocolV1
	}{
		{"socks5-auth", "socks", "fixture-secret-password", []hostresources.LocalProxyProtocolV1{hostresources.LocalProxyProtocolSOCKS5}},
		{"socks4-auth", "socks", "", []hostresources.LocalProxyProtocolV1{hostresources.LocalProxyProtocolSOCKS4}},
		{"http-auth", "http", "fixture-secret-password", []hostresources.LocalProxyProtocolV1{hostresources.LocalProxyProtocolHTTPConnect, hostresources.LocalProxyProtocolHTTPForward}},
		{"mixed-auth", "mixed", "fixture-secret-password", []hostresources.LocalProxyProtocolV1{
			hostresources.LocalProxyProtocolHTTPConnect, hostresources.LocalProxyProtocolHTTPForward, hostresources.LocalProxyProtocolSOCKS5,
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			port := reserveTCPPort(t)
			username, password := "fixture-user", test.password
			tag := test.name + "-proxy-probe"
			inboundOptions := `{"listen":"127.0.0.1","listen_port":` + strconv.Itoa(port) +
				`,"users":[{"username":"` + username + `","password":"` + password + `"}]}`
			config := []byte(`{"log":{"disabled":true},"inbounds":[{"type":"` + test.inboundType + `","tag":"` + tag + `",` +
				strings.TrimPrefix(inboundOptions, "{") + `],"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct"}}`)
			ctx := sb.Context(t.Context(), registry.InboundRegistry(), registry.OutboundRegistry(), registry.EndpointRegistry(), registry.DNSTransportRegistry(), registry.ServiceRegistry())
			var options option.Options
			if err := options.UnmarshalJSONContext(ctx, config); err != nil {
				t.Fatal(err)
			}
			instance, err := corebox.NewBox(corebox.Options{Context: ctx, Options: options})
			if err != nil {
				t.Fatal(err)
			}
			if err = instance.Start(); err != nil {
				_ = instance.Close()
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = instance.Close() })

			inbound := model.Inbound{Id: 81, Type: test.inboundType, Tag: tag, Options: json.RawMessage(inboundOptions)}
			fixture := newPatchFixture(t, inbound)
			fixture.service.mutation.Hydrator = identityProbeHydrator{}
			if err := fixture.db.AutoMigrate(&model.InboundEndpointLease{}); err != nil {
				t.Fatal(err)
			}
			var persisted model.Inbound
			if err := fixture.db.First(&persisted, inbound.Id).Error; err != nil {
				t.Fatal(err)
			}
			content, err := persisted.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			digest, err := canonicalInboundOptionsDigest(t.Context(), content)
			if err != nil {
				t.Fatal(err)
			}
			fixture.runtime.observation.OptionsDigest = digest
			fixture.service.effective = exactEffectiveReader{typ: inbound.Type, tag: inbound.Tag, digest: digest}
			snapshot, err := fixture.service.Snapshot(t.Context(), inbound.Id)
			if err != nil {
				t.Fatal(err)
			}
			if !snapshot.LocalProxy.Authentication.Expected || snapshot.LocalProxy.Authentication.Count != 1 {
				t.Fatalf("authentication shape=%#v", snapshot.LocalProxy.Authentication)
			}
			leaseRevision := hostresources.Revision("local-proxy-probe-lease")
			leaseID := "local-proxy-probe-lease-" + test.name
			endpointID := "endpoint-local-proxy-" + test.name
			if err := fixture.db.Create(&model.InboundEndpointLease{
				LeaseID: leaseID, InboundID: inbound.Id, ProviderID: "core", HolderID: "operation-1",
				ResourceID: snapshot.ResourceID, EndpointID: endpointID, ExactReferenceJSON: []byte(`{}`),
				LeaseJSON: []byte(`{}`), LeaseRevision: leaseRevision,
				State: string(hostresources.EndpointLeaseMutationPending), LastRequestID: "fence-1",
				IssuedAtUnix: time.Now().Add(-time.Minute).Unix(), RenewedAtUnix: time.Now().Unix(),
				ExpiresAtUnix: time.Now().Add(time.Minute).Unix(),
			}).Error; err != nil {
				t.Fatal(err)
			}
			provider := NewLocalProxyProbeProviderV1(fixture.service)
			probes := componenthealth.NewLocalProxyProbeRegistryV1()
			if _, err := probes.Register(provider); err != nil {
				t.Fatal(err)
			}
			revision := hostresources.Revision("local-proxy-probe-binding")
			for _, protocol := range test.protocols {
				target := componenthealth.LocalProxyProbeTargetV1{
					ProviderID: "core", ResourceID: snapshot.ResourceID, EndpointID: endpointID, Protocol: protocol,
					ConfigurationRevision: snapshot.ConfigurationRevision, RuntimeRevision: snapshot.Effective.Revision,
					FactRevision: revision, ListenerObservationRevision: revision,
					AuthenticationRevision: snapshot.LocalProxy.Authentication.Revision,
					TLSRevision:            snapshot.LocalProxy.TLSRevision, SystemProxyRevision: snapshot.LocalProxy.SystemProxyRevision,
					LeaseID: leaseID, LeaseRevision: leaseRevision, LeaseState: hostresources.EndpointLeaseMutationPending,
					OperationID: "operation-1", OperationRevision: 2, PlanRevision: revision, MarkerRevision: revision,
				}
				capability := probes.Capability(t.Context(), target)
				if !capability.Available {
					t.Fatalf("%s capability=%#v", protocol, capability)
				}
				observation, err := probes.ProbeFresh(t.Context(), componenthealth.LocalProxyProbeRequestV1{
					Target: target, ProviderInstance: capability.ProviderInstance,
					NotBeforeUnixNano: time.Now().Add(-time.Millisecond).UnixNano(),
				})
				if err != nil {
					t.Fatalf("%s probe: %v", protocol, err)
				}
				encoded, _ := json.Marshal(observation)
				if !observation.Passed || !observation.PositiveTransaction ||
					!observation.MissingAuthenticationDenied || !observation.InvalidAuthenticationDenied ||
					!observation.ExactTarget || !observation.ExactSink ||
					strings.Contains(string(encoded), username) || password != "" && strings.Contains(string(encoded), password) ||
					strings.Contains(string(encoded), "127.0.0.1") || strings.Contains(string(encoded), strconv.Itoa(port)) {
					t.Fatalf("unsafe or incomplete %s observation: %s", protocol, encoded)
				}
			}
		})
	}
}

func TestLocalProxyNetworkDialerRejectsArbitraryAndUDPTargets(t *testing.T) {
	server := M.ParseSocksaddr("127.0.0.1:1080")
	dialer := localProxyNetworkDialer{server: server}
	if _, err := dialer.DialContext(t.Context(), "tcp", M.ParseSocksaddr("127.0.0.1:1081")); err == nil {
		t.Fatal("arbitrary TCP destination was accepted")
	}
	if _, err := dialer.DialContext(t.Context(), "udp", server); err == nil {
		t.Fatal("UDP destination was accepted")
	}
	if _, err := dialer.ListenPacket(t.Context(), server); err == nil {
		t.Fatal("fixed SOCKS UDP listener was accepted")
	}
}

func reserveTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}
