package coreinboundcontrol

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

type exactEffectiveReader struct{ typ, tag, digest string }

func (f exactEffectiveReader) LookupInbound(string) (bool, string, string, bool) {
	return true, f.typ, f.tag, true
}
func (f exactEffectiveReader) LookupInboundExact(string) (bool, string, string, string, uint64, bool) {
	digest := f.digest
	if digest == "" {
		digest = strings64("a")
	}
	return true, f.typ, f.tag, digest, 7, true
}
func strings64(value string) string {
	result := ""
	for len(result) < 64 {
		result += value
	}
	return result[:64]
}

func TestUDPTransportUsesPinnedOptionSemantics(t *testing.T) {
	tls := &model.Tls{Id: 1, Server: json.RawMessage(`{"enabled":true,"certificate":["secret"],"key":["secret"]}`)}
	cases := []struct {
		name, typ, options, wantNetwork, wantClass string
		auth                                       int
		tls                                        *model.Tls
		tlsID                                      uint
		direct, association                        bool
	}{
		{"direct_default", "direct", `{"listen":"0.0.0.0","listen_port":2001}`, "tcp_udp", "TCP_UDP_DUAL", 0, nil, 0, true, false},
		{"direct_udp", "direct", `{"listen":"0.0.0.0","listen_port":2002,"network":"udp"}`, "udp", "PLAIN_UDP", 0, nil, 0, true, false},
		{"shadowsocks_tcp", "shadowsocks", `{"listen":"0.0.0.0","listen_port":2003,"network":"tcp","method":"2022-blake3-aes-128-gcm","password":"MTIzNDU2Nzg5MDEyMzQ1Ng=="}`, "tcp", "UNSUPPORTED", 1, nil, 0, false, false},
		{"naive_default", "naive", `{"listen":"0.0.0.0","listen_port":2004}`, "tcp_udp", "QUIC_NATIVE", 2, tls, 1, true, false},
		{"hysteria", "hysteria", `{"listen":"0.0.0.0","listen_port":2005}`, "udp", "QUIC_NATIVE", 2, tls, 1, true, false},
		{"hysteria2", "hysteria2", `{"listen":"0.0.0.0","listen_port":2006}`, "udp", "QUIC_NATIVE", 2, tls, 1, true, false},
		{"tuic", "tuic", `{"listen":"0.0.0.0","listen_port":2007,"zero_rtt_handshake":true}`, "udp", "QUIC_NATIVE", 2, tls, 1, true, false},
		{"vless_quic", "vless", `{"listen":"0.0.0.0","listen_port":2008,"transport":{"type":"quic"}}`, "udp", "QUIC_V2RAY_TRANSPORT", 2, tls, 1, true, false},
		{"vmess_quic", "vmess", `{"listen":"0.0.0.0","listen_port":2009,"transport":{"type":"quic"}}`, "udp", "QUIC_V2RAY_TRANSPORT", 2, tls, 1, true, false},
		{"socks_association", "socks", `{"listen":"0.0.0.0","listen_port":2010}`, "tcp", "PROXY_UDP_ASSOCIATION", 2, nil, 0, false, true},
		{"mixed_association", "mixed", `{"listen":"0.0.0.0","listen_port":2011}`, "tcp", "PROXY_UDP_ASSOCIATION", 2, nil, 0, false, true},
	}
	for index, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			inbound := model.Inbound{Id: uint(index + 1), Type: test.typ, Tag: test.name, TlsId: test.tlsID, Tls: test.tls, Options: json.RawMessage(test.options)}
			snapshot := buildSnapshot(inbound, 1, exactIdentity(true), exactEffectiveReader{typ: test.typ, tag: test.name}, test.auth)
			if snapshot.Listener.Network != test.wantNetwork || snapshot.UDPTransport.Class != test.wantClass || snapshot.UDPTransport.DirectSocketActionable != test.direct || snapshot.UDPTransport.DependentAssociation != test.association {
				t.Fatalf("transport=%#v listener=%#v reasons=%#v", snapshot.UDPTransport, snapshot.Listener, snapshot.ReasonCodes)
			}
			if snapshot.Authentication.Count < test.auth {
				t.Fatalf("auth count=%d want >=%d", snapshot.Authentication.Count, test.auth)
			}
		})
	}
}

func TestEffectiveConfigurationRequiresExactRuntimeDigest(t *testing.T) {
	inbound := model.Inbound{Id: 90, Type: "direct", Tag: "exact-runtime", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":2090,"network":"udp"}`)}
	content, err := inbound.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := canonicalInboundOptionsDigest(t.Context(), content)
	if err != nil {
		t.Fatal(err)
	}
	matched := buildSnapshotWithRuntimeDigest(inbound, 0, exactIdentity(true), exactEffectiveReader{typ: inbound.Type, tag: inbound.Tag, digest: expected}, 0, expected)
	mismatched := buildSnapshotWithRuntimeDigest(inbound, 0, exactIdentity(true), exactEffectiveReader{typ: inbound.Type, tag: inbound.Tag, digest: strings64("f")}, 0, expected)
	changedConfiguration := buildSnapshotWithRuntimeDigest(inbound, 0, exactIdentity(true), exactEffectiveReader{typ: inbound.Type, tag: inbound.Tag, digest: expected}, 0, strings64("e"))
	if !matched.Effective.ConfigurationProven || mismatched.Effective.ConfigurationProven {
		t.Fatalf("runtime proof matched=%#v mismatched=%#v", matched.Effective, mismatched.Effective)
	}
	if matched.ConfigurationRevision == changedConfiguration.ConfigurationRevision {
		t.Fatal("effective configured semantics did not change configuration revision")
	}
}

func TestQUICBuildAttestationIsDeterministicAndPinned(t *testing.T) {
	identity := exactIdentity(true)
	left := ReadQUICBuildFeatureV1(identity)
	right := ReadQUICBuildFeatureV1(identity)
	if left.Revision == "" || !reflect.DeepEqual(left, right) || left.SourceRevision != PinnedSingBoxSourceRevision || left.ModuleRevision == "" {
		t.Fatalf("features differ: %#v %#v", left, right)
	}
	unknown := ReadQUICBuildFeatureV1(ResolveRuntimeIdentityV1(RuntimeBuildInputV1{}))
	if unknown.State != BuildFeatureUnknown {
		t.Fatalf("unknown runtime yielded %s", unknown.State)
	}
}
