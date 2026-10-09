package registry

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"
	"github.com/sagernet/sing/service"
)

type fixtureNetwork struct {
	adapter.NetworkManager
	finder control.InterfaceFinder
}

func (f fixtureNetwork) InterfaceFinder() control.InterfaceFinder      { return f.finder }
func (f fixtureNetwork) InterfaceMonitor() tun.DefaultInterfaceMonitor { return nil }

type fixtureFinder struct{ control.InterfaceFinder }

func (fixtureFinder) Interfaces() []control.Interface {
	return []control.Interface{{Name: "loopback", Flags: net.FlagUp | net.FlagMulticast | net.FlagLoopback, Addresses: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/8")}}}
}

func TestMDNSOfficialConstructorAndLocalDomainContract(t *testing.T) {
	ctx := service.ContextWith[adapter.NetworkManager](Context(context.Background()), fixtureNetwork{finder: fixtureFinder{}})
	transport, err := DNSTransportRegistry().CreateDNSTransport(ctx, log.NewNOPFactory().NewLogger("fixture"), "mdns", "mdns", &option.MDNSDNSServerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	for _, test := range []struct {
		domain string
		local  bool
	}{{"Printer.LOCAL", true}, {"1.1.254.169.in-addr.arpa.", true}, {"0.8.e.f.ip6.arpa.", true}, {"local.invalid", false}, {"example.invalid", false}} {
		if transport.(adapter.DNSTransportWithPreferredDomain).PreferredDomain(test.domain) != test.local {
			t.Fatal("official local-domain boundary changed")
		}
	}
	query := new(dns.Msg)
	query.SetQuestion("printer.local.", dns.TypeA)
	bounded, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if _, err := transport.Exchange(bounded, query); err == nil {
		t.Fatal("loopback-only fixture authorized multicast exchange")
	}
	for _, kind := range []string{"mdns", "resolved"} {
		if _, found := DNSTransportRegistry().CreateOptions(kind); !found {
			t.Fatal("selected official DNS schema missing")
		}
	}
}
