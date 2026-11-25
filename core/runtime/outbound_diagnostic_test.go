package runtime

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"

	"github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
)

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
