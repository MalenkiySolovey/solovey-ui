package box

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	mDNS "github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	boxoutbound "github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	sbservice "github.com/sagernet/sing/service"
)

type dnsPathWitness struct {
	boxoutbound.Adapter
	hits *atomic.Int32
}

func TestDNSUpgradeFakeIPAndRcodeBehavior(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "dns-cache.db")
	raw := []byte(fmt.Sprintf(`{"log":{"disabled":true},"dns":{"fakeip":{"enabled":true,"inet4_range":"198.18.0.0/15","inet6_range":"fc00::/18"},"servers":[{"type":"udp","tag":"normal","server":"127.0.0.1"},{"tag":"fake","address":"fakeip"},{"tag":"deny","address":"rcode://name_error"}],"rules":[{"domain_suffix":["blocked.example"],"server":"deny"},{"domain_suffix":["fake.example"],"server":"fake"}]},"experimental":{"cache_file":{"enabled":true,"store_fakeip":true,"path":%q}}}`, cachePath))
	projection, err := singboxconfig.PrepareDNSUpgrade(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw = projection.Candidate
	open := func() (*Box, context.Context) {
		ctx := registry.Context(context.Background())
		var options option.Options
		if err := options.UnmarshalJSONContext(ctx, raw); err != nil {
			t.Fatal(err)
		}
		instance, err := NewBox(Options{Context: ctx, Options: options})
		if err != nil {
			t.Fatal(err)
		}
		if err := instance.Start(); err != nil {
			t.Fatal(err)
		}
		return instance, ctx
	}
	instance, ctx := open()
	manager := sbservice.FromContext[adapter.DNSTransportManager](ctx)
	address, err := manager.FakeIP().Store().Create("fake.example", false)
	if err != nil || !netip.MustParsePrefix("198.18.0.0/15").Contains(address) {
		t.Fatal("FakeIP range changed")
	}
	query := new(mDNS.Msg)
	query.SetQuestion("blocked.example.", mDNS.TypeA)
	response, err := sbservice.FromContext[adapter.DNSRouter](ctx).Exchange(ctx, query, adapter.DNSQueryOptions{})
	if err != nil || response.Rcode != mDNS.RcodeNameError {
		t.Fatal("rcode response changed")
	}
	if err := instance.Close(); err != nil {
		t.Fatal(err)
	}
	instance, ctx = open()
	defer instance.Close()
	store := sbservice.FromContext[adapter.DNSTransportManager](ctx).FakeIP().Store()
	if domain, loaded := store.Lookup(address); !loaded || domain != "fake.example" {
		t.Fatal("FakeIP identity lost across restart")
	}
	if err := store.Reset(); err != nil {
		t.Fatal(err)
	}
	if _, loaded := store.Lookup(address); loaded {
		t.Fatal("FakeIP reset retained old mapping")
	}
	if directory := os.Getenv("SOLOVEY_DNS_BEHAVIOR_CAPTURE"); directory != "" {
		data, _ := json.MarshalIndent(map[string]any{"fakeip_ranges_preserved": true, "restart_mapping_preserved": true, "rcode": response.Rcode, "reset_result": "SUCCESS"}, "", "  ")
		if err := os.WriteFile(filepath.Join(directory, "fakeip-rcode-behavior.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func (o *dnsPathWitness) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	o.hits.Add(1)
	return (&net.Dialer{}).DialContext(ctx, network, destination.String())
}
func (o *dnsPathWitness) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	o.hits.Add(1)
	return (&net.ListenConfig{}).ListenPacket(ctx, "udp", "127.0.0.1:0")
}

// This same driver also runs against the exact accepted checkout. Only its
// context adapter and migration call differ; old/current core traffic counters
// and DNS answers are independent behavioral witnesses, not JSON comparisons.
func TestDNSUpgradeDialPathBehavior(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var received atomic.Int32
	server := &mDNS.Server{PacketConn: conn, Handler: mDNS.HandlerFunc(func(w mDNS.ResponseWriter, request *mDNS.Msg) {
		received.Add(1)
		response := new(mDNS.Msg)
		response.SetReply(request)
		if len(request.Question) > 0 && request.Question[0].Qtype == mDNS.TypeA {
			response.Answer = []mDNS.RR{&mDNS.A{Hdr: mDNS.RR_Header{Name: request.Question[0].Name, Rrtype: mDNS.TypeA, Class: mDNS.ClassINET, Ttl: 60}, A: net.ParseIP("192.0.2.1")}}
		}
		_ = w.WriteMsg(response)
	})}
	go func() { _ = server.ActivateAndServe() }()
	t.Cleanup(func() { _ = server.Shutdown(); _ = conn.Close() })
	var report []map[string]any
	for _, test := range []struct {
		name, detour string
		expected     int32
	}{{"explicit-proxy", "proxy", 1}, {"implicit-final-proxy", "", 1}, {"explicit-direct", "direct", 0}} {
		t.Run(test.name, func(t *testing.T) {
			ctx := registry.Context(context.Background())
			outboundRegistry := registry.OutboundRegistry()
			var proxyHits atomic.Int32
			boxoutbound.Register[struct{}](outboundRegistry, "witness", func(_ context.Context, _ adapter.Router, _ log.ContextLogger, tag string, _ struct{}) (adapter.Outbound, error) {
				return &dnsPathWitness{Adapter: boxoutbound.NewAdapter("witness", tag, []string{"tcp", "udp"}, nil), hits: &proxyHits}, nil
			})
			ctx = sbservice.ContextWith[option.OutboundOptionsRegistry](ctx, outboundRegistry)
			ctx = sbservice.ContextWith[adapter.OutboundRegistry](ctx, outboundRegistry)
			raw := []byte(fmt.Sprintf(`{"log":{"disabled":true},"dns":{"servers":[{"tag":"test-dns","address":"udp://%s","detour":%q}],"disable_cache":true},"route":{"final":"proxy"},"outbounds":[{"type":"direct","tag":"direct"},{"type":"witness","tag":"proxy"}]}`, conn.LocalAddr().String(), test.detour))
			projection, err := singboxconfig.PrepareDNSUpgrade(raw)
			if err != nil {
				t.Fatal(err)
			}
			raw = projection.Candidate
			var options option.Options
			if err := options.UnmarshalJSONContext(ctx, raw); err != nil {
				t.Fatal(err)
			}
			instance, err := NewBox(Options{Context: ctx, Options: options})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = instance.Close() })
			if err := instance.Start(); err != nil {
				t.Fatal(err)
			}
			router := sbservice.FromContext[adapter.DNSRouter](ctx)
			query := new(mDNS.Msg)
			query.SetQuestion("migration.example.", mDNS.TypeA)
			queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			response, err := router.Exchange(queryCtx, query, adapter.DNSQueryOptions{})
			if err != nil || len(response.Answer) != 1 || response.Answer[0].(*mDNS.A).A.String() != "192.0.2.1" {
				t.Fatalf("DNS response mismatch: %v", err)
			}
			if proxyHits.Load() != test.expected {
				t.Fatalf("proxy path count=%d want=%d", proxyHits.Load(), test.expected)
			}
			report = append(report, map[string]any{"case": test.name, "proxy_path_calls": proxyHits.Load(), "answer": "192.0.2.1", "rcode": response.Rcode})
		})
	}
	if received.Load() != 3 {
		t.Fatalf("direct/local server received %d requests", received.Load())
	}
	if directory := os.Getenv("SOLOVEY_DNS_BEHAVIOR_CAPTURE"); directory != "" {
		data, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(filepath.Join(directory, "dial-path-behavior.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
