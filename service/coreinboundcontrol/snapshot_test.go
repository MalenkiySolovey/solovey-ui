package coreinboundcontrol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

type fakeEffectiveReader struct {
	available bool
	typ       string
	tag       string
	present   bool
}

func (f fakeEffectiveReader) LookupInbound(string) (bool, string, string, bool) {
	return f.available, f.typ, f.tag, f.present
}

func vlessRealityInbound() model.Inbound {
	return model.Inbound{
		Id: 17, Type: "vless", Tag: "public-vless", TlsId: 3,
		Options: json.RawMessage(`{"listen":"0.0.0.0","listen_port":443}`),
		Tls: &model.Tls{Id: 3, Server: json.RawMessage(`{
			"enabled":true,
			"server_name":"example.com",
			"reality":{"enabled":true,"handshake":{"server":"127.0.0.1","server_port":8443},"private_key":"private-value","short_id":["00"]}
		}`)},
	}
}

func trojanInbound() model.Inbound {
	return model.Inbound{
		Id: 18, Type: "trojan", Tag: "public-trojan", TlsId: 4,
		Options: json.RawMessage(`{
			"listen":"::","listen_port":443,
			"fallback":{"server":"127.0.0.1","server_port":8080},
			"fallback_for_alpn":{"h2":{"server":"127.0.0.1","server_port":8081},"http/1.1":{"server":"127.0.0.1","server_port":8080}}
		}`),
		Tls: &model.Tls{Id: 4, Server: json.RawMessage(`{"enabled":true,"alpn":["http/1.1","h2"],"certificate":["certificate-secret"],"key":["key-secret"]}`)},
	}
}

func TestSnapshotExtractsExactVLESSRealityAndTrojanShapes(t *testing.T) {
	vless := buildSnapshot(vlessRealityInbound(), 1, exactIdentity(true), nil)
	if vless.Listener.Network != "tcp" || vless.Listener.AddressFamily != "ipv4" || vless.Listener.Port != 443 ||
		!vless.TLS.Enabled || !vless.TLS.Reality.Enabled || vless.TLS.Reality.Handshake.Kind != "tcp_host_port" ||
		vless.Capability.Disposition != CapabilitySupportedNaturalFallback {
		t.Fatalf("VLESS snapshot = %#v", vless)
	}
	trojan := buildSnapshot(trojanInbound(), 1, exactIdentity(true), nil)
	if trojan.Listener.AddressFamily != "ipv6" || trojan.DefaultFallback.Kind != "tcp_host_port" || len(trojan.ALPNFallbacks) != 2 ||
		trojan.Capability.Disposition != CapabilitySupported || trojan.Capability.Variant != NativeFallbackTrojanDefaultALPNTCP {
		t.Fatalf("Trojan snapshot = %#v", trojan)
	}
}

func TestSnapshotExtractsPinnedRedirectAndTProxySemantics(t *testing.T) {
	tests := []struct {
		name             string
		inbound          model.Inbound
		wantNetworks     []string
		wantKind         string
		wantTransparent  bool
		wantPolicyRoute  bool
		wantBoundedUDP   bool
		wantOriginalMode string
	}{
		{
			name: "redirect tcp",
			inbound: model.Inbound{Id: 51, Type: "redirect", Tag: "redirect-ingress",
				Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":15001}`)},
			wantNetworks: []string{"tcp"}, wantKind: "redirect",
			wantOriginalMode: "SO_ORIGINAL_DST",
		},
		{
			name: "tproxy explicit tcp",
			inbound: model.Inbound{Id: 52, Type: "tproxy", Tag: "tproxy-tcp",
				Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":15002,"network":"tcp"}`)},
			wantNetworks: []string{"tcp"}, wantKind: "tproxy", wantTransparent: true, wantPolicyRoute: true,
			wantOriginalMode: "IP_TRANSPARENT_RECVORIGDSTADDR",
		},
		{
			name: "tproxy explicit udp",
			inbound: model.Inbound{Id: 53, Type: "tproxy", Tag: "tproxy-udp",
				Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":15003,"network":["udp"]}`)},
			wantNetworks: []string{"udp"}, wantKind: "tproxy", wantTransparent: true, wantPolicyRoute: true, wantBoundedUDP: true,
			wantOriginalMode: "IP_TRANSPARENT_RECVORIGDSTADDR",
		},
		{
			name: "tproxy default tcp udp",
			inbound: model.Inbound{Id: 54, Type: "tproxy", Tag: "tproxy-default",
				Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":15004}`)},
			wantNetworks: []string{"tcp", "udp"}, wantKind: "tproxy", wantTransparent: true, wantPolicyRoute: true, wantBoundedUDP: true,
			wantOriginalMode: "IP_TRANSPARENT_RECVORIGDSTADDR",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := buildSnapshot(test.inbound, 0, exactIdentity(true), nil)
			shape := snapshot.Interception
			if !shape.Candidate || shape.Kind != test.wantKind || !reflect.DeepEqual(shape.EffectiveNetworks, test.wantNetworks) ||
				shape.TransparentSocketRequired != test.wantTransparent || shape.PolicyRoutingRequired != test.wantPolicyRoute ||
				shape.BoundedUDPFlowState != test.wantBoundedUDP || shape.OriginalDestinationMechanism != test.wantOriginalMode ||
				!shape.LinuxOnly || !shape.OriginalDestinationPreserved || !shape.SourcePreserved ||
				shape.LocalOutputCapture || shape.TUNOwned || len(shape.EffectiveNetworksRevision) != 64 ||
				len(shape.SemanticRevision) != 64 {
				t.Fatalf("interception shape = %#v", shape)
			}
		})
	}
}

func TestCompleteRelevantConfigurationChangesRevision(t *testing.T) {
	base := vlessRealityInbound()
	first := buildSnapshot(base, 1, exactIdentity(true), nil)
	base.Options = json.RawMessage(`{"listen":"0.0.0.0","listen_port":443,"tcp_fast_open":true}`)
	second := buildSnapshot(base, 1, exactIdentity(true), nil)
	if first.ConfigurationRevision == second.ConfigurationRevision || first.InboundOptionsDigest == second.InboundOptionsDigest {
		t.Fatal("relevant inbound option change did not alter revision")
	}
}

func TestMapOrderingAndSemanticJSONDoNotChangeRevision(t *testing.T) {
	left := vlessRealityInbound()
	right := vlessRealityInbound()
	right.Options = json.RawMessage(`{ "listen_port" : 443.0, "listen" : "0.0.0.0" }`)
	right.Tls.Server = json.RawMessage(`{
		"reality":{"short_id":["00"],"private_key":"private-value","handshake":{"server_port":8443.0,"server":"127.0.0.1"},"enabled":true},
		"server_name":"example.com","enabled":true
	}`)
	first := buildSnapshot(left, 1, exactIdentity(true), nil)
	second := buildSnapshot(right, 1, exactIdentity(true), nil)
	if first.ConfigurationRevision != second.ConfigurationRevision {
		t.Fatalf("semantic revisions differ: %s != %s; reasons=%#v", first.ConfigurationRevision, second.ConfigurationRevision, second.ReasonCodes)
	}
}

func TestRuntimeAndLabIndependentFactsDoNotChangeConfigurationRevision(t *testing.T) {
	inbound := vlessRealityInbound()
	left := buildSnapshot(inbound, 1, exactIdentity(true), fakeEffectiveReader{available: true, typ: "vless", tag: inbound.Tag, present: true})
	right := buildSnapshot(inbound, 1, exactIdentity(false), fakeEffectiveReader{available: false})
	if left.ConfigurationRevision != right.ConfigurationRevision {
		t.Fatalf("runtime fact changed configuration revision: %s != %s", left.ConfigurationRevision, right.ConfigurationRevision)
	}
	if left.RuntimeIdentityRevision == right.RuntimeIdentityRevision {
		t.Fatal("test did not vary the independent runtime identity")
	}
}

func TestTLSReferenceIdentityDigestAndCountChangeRevision(t *testing.T) {
	base := vlessRealityInbound()
	first := buildSnapshot(base, 1, exactIdentity(true), nil)
	base.Tls.Server = json.RawMessage(`{"enabled":true,"server_name":"changed.example","reality":{"enabled":true,"handshake":{"server":"127.0.0.1","server_port":8443},"private_key":"private-value"}}`)
	second := buildSnapshot(base, 1, exactIdentity(true), nil)
	if first.ConfigurationRevision == second.ConfigurationRevision || first.TLSOptionsDigest == second.TLSOptionsDigest {
		t.Fatal("TLS digest change did not alter configuration revision")
	}
	third := buildSnapshot(base, 2, exactIdentity(true), nil)
	if second.ConfigurationRevision == third.ConfigurationRevision {
		t.Fatal("TLS reference count change did not alter configuration revision")
	}
	base.TlsId, base.Tls.Id = 9, 9
	fourth := buildSnapshot(base, 2, exactIdentity(true), nil)
	if third.ConfigurationRevision == fourth.ConfigurationRevision {
		t.Fatal("TLS reference identity change did not alter configuration revision")
	}
}

func TestSnapshotOutwardJSONContainsNoSecretsOrPaths(t *testing.T) {
	inbound := vlessRealityInbound()
	inbound.Options = json.RawMessage(`{
		"listen":"0.0.0.0","listen_port":443,
		"users":[{"name":"user-name-secret","uuid":"uuid-secret-value"}]
	}`)
	inbound.Tls.Server = json.RawMessage(`{
		"enabled":true,"server_name":"example.com","alpn":["C:\\\\private\\\\alpn","/private/alpn-secret"],"certificate_path":"C:\\\\private\\\\certificate.pem","key_path":"/private/key.pem",
		"reality":{"enabled":true,"handshake":{"server":"127.0.0.1","server_port":8443},"private_key":"reality-private-secret","short_id":["secret-short-id"]}
	}`)
	snapshot := buildSnapshot(inbound, 1, exactIdentity(true), nil)
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"uuid-secret-value", "user-name-secret", "reality-private-secret", "secret-short-id", "certificate.pem", "/private/key.pem", "private\\\\alpn", "/private/alpn-secret"} {
		if strings.Contains(string(payload), secret) {
			t.Fatalf("secret or path leaked in snapshot JSON: %s", secret)
		}
	}
}

func TestSnapshotBindsRealityServerNameWithoutExposingIt(t *testing.T) {
	inbound := vlessRealityInbound()
	inbound.Tls.Server = json.RawMessage(`{"enabled":true,"server_name":"Decoy.Example","reality":{"enabled":true,"handshake":{"server":"example.com","server_port":443},"private_key":"secret","short_id":["0123456789abcdef"]}}`)
	snapshot := buildSnapshot(inbound, 1, exactIdentity(true), nil)
	want := digestValue(struct {
		Schema     string
		ServerName string
	}{"solovey-ui/inbound-tls-server-name/v1", "decoy.example"})
	if snapshot.TLS.ServerNameDigest != want {
		t.Fatalf("REALITY server name proof = %q, want canonical DNS proof %q", snapshot.TLS.ServerNameDigest, want)
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(payload)), "decoy.example") {
		t.Fatalf("REALITY server name leaked from snapshot: %s", payload)
	}
}

func TestSnapshotPreservesNeutralProxyAndAuthenticationFactsWithoutSecrets(t *testing.T) {
	inbound := model.Inbound{
		Id: 19, Type: "mixed", Tag: "mixed-auth",
		Options: json.RawMessage(`{
			"listen":"127.0.0.1","listen_port":1080,"proxy_protocol":true,
			"users":[{"username":"neutral-user-secret","password":"neutral-password-secret"}]
		}`),
	}
	snapshot := buildSnapshot(inbound, 0, exactIdentity(true), nil)
	if !snapshot.Listener.ProxyProtocol || !snapshot.Authentication.Known || !snapshot.Authentication.Expected {
		t.Fatalf("neutral facts = %#v %#v", snapshot.Listener, snapshot.Authentication)
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"neutral-user-secret", "neutral-password-secret"} {
		if strings.Contains(string(payload), secret) {
			t.Fatalf("authentication secret leaked in snapshot JSON: %s", secret)
		}
	}

	inbound.Options = json.RawMessage(`{"listen":"127.0.0.1","listen_port":1080}`)
	snapshot = buildSnapshot(inbound, 0, exactIdentity(true), nil)
	if !snapshot.Authentication.Known || snapshot.Authentication.Expected {
		t.Fatalf("empty authentication facts = %#v", snapshot.Authentication)
	}
}

func TestSnapshotExtractsExactSOCKSHTTPAndMixedLocalProxyShapes(t *testing.T) {
	tests := []struct {
		name         string
		inbound      model.Inbound
		protocols    []string
		authExpected bool
		socks4Auth   bool
		tls          bool
		systemProxy  bool
		dependentUDP bool
	}{
		{
			name: "SOCKS no auth",
			inbound: model.Inbound{Id: 31, Type: "socks", Tag: "socks-local",
				Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":1080}`)},
			protocols: []string{"SOCKS4", "SOCKS5"}, dependentUDP: true,
		},
		{
			name: "SOCKS username password",
			inbound: model.Inbound{Id: 32, Type: "socks", Tag: "socks-auth",
				Options: json.RawMessage(`{"listen":"10.0.0.8","listen_port":1080,"users":[{"username":"alice","password":"secret"}]}`)},
			protocols: []string{"SOCKS5"}, authExpected: true, dependentUDP: true,
		},
		{
			name: "SOCKS4 compatible auth",
			inbound: model.Inbound{Id: 33, Type: "socks", Tag: "socks4-auth",
				Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":1080,"users":[{"username":"alice","password":""}]}`)},
			protocols: []string{"SOCKS4", "SOCKS5"}, authExpected: true, socks4Auth: true, dependentUDP: true,
		},
		{
			name: "HTTP TLS",
			inbound: model.Inbound{Id: 34, Type: "http", Tag: "http-auth",
				Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":8080,"users":[{"username":"alice","password":"secret"}],"tls":{"enabled":true,"server_name":"proxy.example"}}`)},
			protocols: []string{"HTTP_CONNECT", "HTTP_FORWARD"}, authExpected: true, tls: true,
		},
		{
			name: "Mixed exact shared listener",
			inbound: model.Inbound{Id: 35, Type: "mixed", Tag: "mixed-local",
				Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":1081}`)},
			protocols: []string{"HTTP_CONNECT", "HTTP_FORWARD", "SOCKS4", "SOCKS5"}, dependentUDP: true,
		},
		{
			name: "HTTP system proxy diagnostic",
			inbound: model.Inbound{Id: 36, Type: "http", Tag: "http-system",
				Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":8081,"set_system_proxy":true}`)},
			protocols: []string{"HTTP_CONNECT", "HTTP_FORWARD"}, systemProxy: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := buildSnapshot(test.inbound, 0, exactIdentity(true), nil)
			if !snapshot.LocalProxy.Candidate || !reflect.DeepEqual(snapshot.LocalProxy.Protocols, test.protocols) ||
				snapshot.LocalProxy.Authentication.Expected != test.authExpected ||
				snapshot.LocalProxy.SOCKS4Authenticated != test.socks4Auth ||
				snapshot.LocalProxy.TLS.Enabled != test.tls ||
				snapshot.LocalProxy.SystemProxyEnabled != test.systemProxy ||
				snapshot.LocalProxy.DependentUDPAssociation != test.dependentUDP ||
				snapshot.LocalProxy.StaticUDPListener ||
				snapshot.LocalProxy.ArbitraryTargetConfigurable ||
				snapshot.LocalProxy.ManagementTargetConfigurable {
				t.Fatalf("local proxy shape = %#v", snapshot.LocalProxy)
			}
			if snapshot.LocalProxy.ProtocolRevision == "" || snapshot.LocalProxy.Authentication.Revision == "" ||
				snapshot.LocalProxy.TLSRevision == "" || snapshot.LocalProxy.SystemProxyRevision == "" {
				t.Fatalf("missing secret-free shape revisions: %#v", snapshot.LocalProxy)
			}
		})
	}
}

func TestLocalProxyShapeRevisionIsSecretFreeAndTracksSemanticDrift(t *testing.T) {
	firstInbound := model.Inbound{Id: 40, Type: "mixed", Tag: "mixed-auth",
		Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":1080,"users":[{"username":"alice","password":"first-secret"}]}`)}
	secondInbound := firstInbound
	secondInbound.Options = json.RawMessage(`{"listen":"127.0.0.1","listen_port":1080,"users":[{"username":"bob","password":"second-secret"}]}`)
	first := buildSnapshot(firstInbound, 0, exactIdentity(true), nil)
	second := buildSnapshot(secondInbound, 0, exactIdentity(true), nil)
	if first.LocalProxy.ProtocolRevision != second.LocalProxy.ProtocolRevision ||
		first.LocalProxy.Authentication.Revision != second.LocalProxy.Authentication.Revision {
		t.Fatal("credential values changed the secret-free capability/auth shape revision")
	}
	payload, err := json.Marshal([]InboundFallbackSnapshotV1{first, second})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"alice", "bob", "first-secret", "second-secret"} {
		if strings.Contains(string(payload), secret) {
			t.Fatalf("credential leaked from snapshot: %q", secret)
		}
	}
	drifted := firstInbound
	drifted.Options = json.RawMessage(`{"listen":"127.0.0.1","listen_port":1080}`)
	third := buildSnapshot(drifted, 0, exactIdentity(true), nil)
	if third.LocalProxy.Authentication.Revision == first.LocalProxy.Authentication.Revision {
		t.Fatal("authentication presence drift did not change semantic revision")
	}
}

func TestEffectiveTagPresenceAloneDoesNotProveConfiguration(t *testing.T) {
	inbound := vlessRealityInbound()
	snapshot := buildSnapshot(inbound, 1, exactIdentity(true), fakeEffectiveReader{available: true, typ: inbound.Type, tag: inbound.Tag, present: true})
	if !snapshot.Effective.Present || snapshot.Effective.ConfigurationProven || snapshot.Effective.Revision == "" ||
		!containsReason(snapshot.Effective.ReasonCodes, ReasonEffectiveConfigurationUnproven) {
		t.Fatalf("effective fact = %#v", snapshot.Effective)
	}
}

func TestMalformedIncompleteAndUnknownOptionsRemainUnknown(t *testing.T) {
	inbound := vlessRealityInbound()
	inbound.Options = json.RawMessage(`{"listen":"0.0.0.0","listen_port":443,"future_option":true}`)
	snapshot := buildSnapshot(inbound, 1, exactIdentity(true), nil)
	if snapshot.Capability.Disposition != CapabilityUnknown || !containsReason(snapshot.ReasonCodes, ReasonInboundShapeUnknown) {
		t.Fatalf("unknown option snapshot = %#v", snapshot)
	}
	inbound.Options = json.RawMessage(`{"listen":`)
	snapshot = buildSnapshot(inbound, 1, exactIdentity(true), nil)
	if snapshot.Capability.Disposition != CapabilityUnknown || !containsReason(snapshot.ReasonCodes, ReasonInboundOptionsMalformed) {
		t.Fatalf("malformed snapshot = %#v", snapshot)
	}
	inbound = vlessRealityInbound()
	inbound.Tls = nil
	snapshot = buildSnapshot(inbound, 1, exactIdentity(true), nil)
	if snapshot.Capability.Disposition != CapabilityUnknown || !containsReason(snapshot.ReasonCodes, ReasonTLSReferenceMissing) {
		t.Fatalf("missing TLS snapshot = %#v", snapshot)
	}
}

func TestServiceExposesOnlyNarrowReadAndFallbackMutationMethods(t *testing.T) {
	serviceType := reflect.TypeFor[*Service]()
	methods := make([]string, 0, serviceType.NumMethod())
	for index := 0; index < serviceType.NumMethod(); index++ {
		methods = append(methods, serviceType.Method(index).Name)
	}
	if !reflect.DeepEqual(methods, []string{
		"ApplyFallbackPatch", "FindCheckpoint", "Identity", "InspectCheckpoint", "ListSnapshots", "PrepareCheckpoint", "PreviewFallbackPatch",
		"ReleaseCheckpoint", "RestoreCheckpoint", "Snapshot", "VerifyEffective",
	}) {
		t.Fatalf("service methods = %#v", methods)
	}
}
