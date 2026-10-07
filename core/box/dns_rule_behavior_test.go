package box

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	mDNS "github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	boxdns "github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	sbservice "github.com/sagernet/sing/service"
)

type dnsRuleWitness struct {
	boxdns.TransportAdapter
	address string
}

func (t *dnsRuleWitness) Start(adapter.StartStage) error { return nil }
func (t *dnsRuleWitness) Close() error                   { return nil }
func (t *dnsRuleWitness) Reset()                         {}
func (t *dnsRuleWitness) Exchange(_ context.Context, q *mDNS.Msg) (*mDNS.Msg, error) {
	r := new(mDNS.Msg)
	r.SetReply(q)
	if q.Question[0].Qtype == mDNS.TypeA {
		r.Answer = []mDNS.RR{&mDNS.A{Hdr: mDNS.RR_Header{Name: q.Question[0].Name, Rrtype: mDNS.TypeA, Class: mDNS.ClassINET, Ttl: 60}, A: net.ParseIP(t.address).To4()}}
	}
	return r, nil
}
func (t *dnsRuleWitness) ExchangeAsync(ctx context.Context, q *mDNS.Msg, cb func(*mDNS.Msg, error)) {
	r, e := t.Exchange(ctx, q)
	cb(r, e)
}

func openDNSRuleWitness(t *testing.T, rules, sets string) (context.Context, adapter.DNSRouter, adapter.DNSTransportManager) {
	t.Helper()
	ctx := registry.Context(context.Background())
	dnsRegistry := registry.DNSTransportRegistry()
	boxdns.RegisterTransport[struct{}](dnsRegistry, "rule-witness", func(_ context.Context, _ log.ContextLogger, tag string, _ struct{}) (adapter.DNSTransport, error) {
		address := "192.0.2.1"
		if tag == "b" {
			address = "198.51.100.2"
		}
		return &dnsRuleWitness{TransportAdapter: boxdns.NewTransportAdapter("rule-witness", tag, nil), address: address}, nil
	})
	ctx = sbservice.ContextWith[option.DNSTransportOptionsRegistry](ctx, dnsRegistry)
	ctx = sbservice.ContextWith[adapter.DNSTransportRegistry](ctx, dnsRegistry)
	if sets == "" {
		sets = "[]"
	}
	raw := []byte(fmt.Sprintf(`{"log":{"disabled":true},"dns":{"servers":[{"type":"rule-witness","tag":"a"},{"type":"rule-witness","tag":"b"}],"final":"b","disable_cache":true,"rules":%s},"route":{"rule_set":%s}}`, rules, sets))
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
	return ctx, sbservice.FromContext[adapter.DNSRouter](ctx), sbservice.FromContext[adapter.DNSTransportManager](ctx)
}

func TestDNSUpgradeLegacyFilterAndInternalBehavior(t *testing.T) {
	var report []map[string]any
	for _, test := range []struct{ name, rules, sets string }{
		{"inverted-address-filter", `[{"ip_cidr":["192.0.2.0/24"],"invert":true,"server":"a"},{"server":"b"}]`, ""},
		{"pure-ip-set", `[{"rule_set":["ip"],"server":"a"},{"server":"b"}]`, `[{"type":"inline","tag":"ip","rules":[{"ip_cidr":["192.0.2.0/24"]}]}]`},
		{"accept-empty", `[{"rule_set":["domains"],"rule_set_ip_cidr_accept_empty":true,"server":"a"},{"server":"b"}]`, `[{"type":"inline","tag":"domains","rules":[{"domain_suffix":["example"]}]}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, router, _ := openDNSRuleWitness(t, test.rules, test.sets)
			ctx = adapter.WithContext(ctx, &adapter.InboundContext{Source: M.Socksaddr{Addr: netip.MustParseAddr("192.0.2.5"), Port: 1234}})
			q := new(mDNS.Msg)
			q.SetQuestion("rules.example.", mDNS.TypeA)
			r, err := router.Exchange(ctx, q, adapter.DNSQueryOptions{})
			if err != nil || len(r.Answer) != 1 {
				t.Fatalf("query failed: %v", err)
			}
			answer := r.Answer[0].(*mDNS.A).A.String()
			expected := "192.0.2.1"
			if test.name == "inverted-address-filter" {
				expected = "198.51.100.2"
			}
			if answer != expected {
				t.Fatalf("rule result=%s expected=%s", answer, expected)
			}
			report = append(report, map[string]any{"case": test.name, "answer": answer})
		})
	}
	t.Run("query-internal-and-explicit", func(t *testing.T) {
		ctx, router, manager := openDNSRuleWitness(t, `[{"query_type":["A"],"server":"a"}]`, "")
		addresses, err := router.Lookup(ctx, "internal.example", adapter.DNSQueryOptions{Strategy: C.DomainStrategyIPv4Only})
		if err != nil || len(addresses) != 1 {
			t.Fatalf("internal lookup failed: %v; addresses=%v", err, addresses)
		}
		// Current internal queries intentionally match query_type. The accepted
		// driver records its old bypass result separately, without forcing it.
		if addresses[0].String() != "192.0.2.1" {
			t.Fatal("current internal query did not match")
		}
		transport, _ := manager.Transport("b")
		explicit, err := router.Lookup(ctx, "internal.example", adapter.DNSQueryOptions{Transport: transport, Strategy: C.DomainStrategyIPv4Only})
		if err != nil || len(explicit) != 1 || explicit[0].String() != "198.51.100.2" {
			t.Fatal("explicit resolver bypass changed")
		}
		report = append(report, map[string]any{"case": "query-internal-and-explicit", "internal_answer": addresses[0].String(), "explicit_answer": explicit[0].String()})
	})
	if directory := os.Getenv("SOLOVEY_DNS_BEHAVIOR_CAPTURE"); directory != "" {
		data, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(filepath.Join(directory, "rule-behavior.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDNSUpgradeEvaluateResponseRaceBehavior(t *testing.T) {
	ctx, router, _ := openDNSRuleWitness(t, `[{"action":"evaluate","tag":"first","server":"a"},{"match_response":"first","ip_cidr":["192.0.2.0/24"],"action":"respond","race":true}]`, "")
	q := new(mDNS.Msg)
	q.SetQuestion("race.example.", mDNS.TypeA)
	r, err := router.Exchange(ctx, q, adapter.DNSQueryOptions{})
	if err != nil || len(r.Answer) != 1 || r.Answer[0].(*mDNS.A).A.String() != "192.0.2.1" {
		t.Fatalf("evaluate/respond/race result changed: %v", err)
	}
}
