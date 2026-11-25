package runtime

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"

	"github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
)

type diagnosticOutboundFixture struct {
	adapter.Outbound
	kind     string
	networks []string
}

func (f diagnosticOutboundFixture) Type() string      { return f.kind }
func (f diagnosticOutboundFixture) Network() []string { return f.networks }

func TestDiagnosticTCPModeDistinguishesProxyStreamFromAcknowledgement(t *testing.T) {
	for _, test := range []struct {
		kind     string
		networks []string
		want     DiagnosticTCPMode
	}{
		{C.TypeDirect, []string{"tcp", "udp"}, DiagnosticTCPDirect},
		{C.TypeHTTP, []string{"tcp"}, DiagnosticTCPProxyConnect},
		{C.TypeSOCKS, []string{"tcp", "udp"}, DiagnosticTCPProxyConnect},
		{C.TypeSnell, []string{"tcp", "udp"}, DiagnosticTCPProxyStream},
		{C.TypeSnell, []string{"udp"}, DiagnosticTCPUnavailable},
		{C.TypeSelector, []string{"tcp"}, DiagnosticTCPUnavailable},
	} {
		if got := diagnosticTCPMode(diagnosticOutboundFixture{kind: test.kind, networks: test.networks}); got != test.want {
			t.Errorf("type %s with networks %v: got %d, want %d", test.kind, test.networks, got, test.want)
		}
	}
}

type diagnosticDNSFixture struct {
	adapter.DNSRouter
	exchange func(*dns.Msg, adapter.DNSQueryOptions) (*dns.Msg, error)
}

func (f diagnosticDNSFixture) Exchange(_ context.Context, request *dns.Msg, options adapter.DNSQueryOptions) (*dns.Msg, error) {
	return f.exchange(request, options)
}

type diagnosticTransportFixture struct{ adapter.DNSTransport }

func TestBlockCheckExplicitDNSKeepsServerAndWireStatus(t *testing.T) {
	transport := &diagnosticTransportFixture{}
	var exchanges int
	owner := diagnosticDNSFixture{exchange: func(request *dns.Msg, options adapter.DNSQueryOptions) (*dns.Msg, error) {
		exchanges++
		if options.Transport != transport || !options.DisableCache || !options.DisableOptimisticCache || len(request.Question) != 1 || request.Question[0].Name != "fixture.example." {
			t.Fatalf("DNS owner request changed: %+v %v", options, request.Question)
		}
		response := new(dns.Msg).SetReply(request)
		if request.Question[0].Qtype == dns.TypeA {
			response.Answer = []dns.RR{&dns.A{A: net.IPv4(192, 0, 2, 10)}}
		} else {
			response.Rcode = dns.RcodeNameError
		}
		return response, nil
	}}
	result, err := explicitDiagnosticResolution(t.Context(), owner, transport, "fixture.example", "any", "doh")
	if err != nil || exchanges != 2 || result.RCode != nil || result.ConfiguredTransport != "doh" || len(result.Addresses) != 1 || result.Addresses[0].Unmap().String() != "192.0.2.10" {
		t.Fatalf("different families were reported as one RCODE: %+v %v", result, err)
	}
	result, err = explicitDiagnosticResolution(t.Context(), owner, transport, "fixture.example", "ipv6", "doh")
	if err != nil || exchanges != 3 || result.RCode == nil || *result.RCode != dns.RcodeNameError || len(result.Addresses) != 0 {
		t.Fatalf("negative DNS response was lost: %+v %v", result, err)
	}
}

func TestBlockCheckRefusesPrivateAndUnapprovedDestinationBeforeCoreDial(t *testing.T) {
	diagnostic := OutboundDiagnostic{core: new(Core)}
	for _, destination := range []string{"127.0.0.1:80", "[::ffff:169.254.169.254]:443", "[2001:4860::1%fixture]:443", "8.8.8.8:22"} {
		if _, err := diagnostic.DialTCP(t.Context(), netip.MustParseAddrPort(destination), func(netip.Addr) bool { return true }); !errors.Is(err, ErrDiagnosticTarget) {
			t.Fatalf("unsupported destination was admitted: %s, %v", destination, err)
		}
	}
	if _, err := diagnostic.DialTCP(t.Context(), netip.MustParseAddrPort("8.8.8.8:443"), func(netip.Addr) bool { return false }); !errors.Is(err, ErrDiagnosticTarget) {
		t.Fatalf("host destination policy ignored: %v", err)
	}
}

func TestDiagnosticUDPRequiresAdmittedTargetAndConnectedTransport(t *testing.T) {
	permits := func(netip.Addr) bool { return true }
	for _, tc := range []struct {
		kind     string
		networks []string
		want     bool
	}{
		{C.TypeDirect, []string{"udp", "tcp"}, true},
		{C.TypeDirect, []string{"tcp"}, false},
		{C.TypeSOCKS, []string{"tcp", "udp"}, false},
		{C.TypeHTTP, []string{"tcp"}, false},
		{C.TypeSnell, []string{"tcp", "udp"}, false},
		{C.TypeSelector, []string{"tcp", "udp"}, false},
	} {
		diagnostic := OutboundDiagnostic{core: new(Core), outbound: diagnosticOutboundFixture{kind: tc.kind, networks: tc.networks}}
		if got := diagnostic.SupportsUDP(); got != tc.want {
			t.Errorf("type %s with networks %v: got %t, want %t", tc.kind, tc.networks, got, tc.want)
		}
		for _, destination := range []string{"127.0.0.1:3478", "[::ffff:169.254.169.254]:19302", "[2001:4860::1%fixture]:443", "8.8.8.8:80", "8.8.8.8:3479"} {
			if _, err := diagnostic.DialUDP(t.Context(), netip.MustParseAddrPort(destination), permits); !errors.Is(err, ErrDiagnosticTarget) {
				t.Fatalf("unsupported UDP destination was admitted: %s, %v", destination, err)
			}
		}
		for _, destination := range []string{"8.8.8.8:443", "8.8.8.8:3478", "[2001:4860::1]:19302"} {
			target := netip.MustParseAddrPort(destination)
			if _, err := diagnostic.DialUDP(t.Context(), target, func(netip.Addr) bool { return false }); !errors.Is(err, ErrDiagnosticTarget) {
				t.Fatalf("host UDP destination policy ignored for %s: %v", destination, err)
			}
			if !tc.want {
				if _, err := diagnostic.DialUDP(t.Context(), target, permits); !errors.Is(err, ErrDiagnosticDatagram) {
					t.Fatalf("unqualified UDP outbound was admitted: %s, %s, %v", tc.kind, destination, err)
				}
			}
		}
	}
}
