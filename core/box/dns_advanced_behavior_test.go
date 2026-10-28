package box

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	mDNS "github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	sbservice "github.com/sagernet/sing/service"
)

func advancedDNSQuery(domain string, subnet, dnssec bool) *mDNS.Msg {
	query := new(mDNS.Msg)
	query.SetQuestion(domain, mDNS.TypeA)
	query.SetEdns0(1232, dnssec)
	if subnet {
		query.IsEdns0().Option = append(query.IsEdns0().Option, &mDNS.EDNS0_SUBNET{Code: mDNS.EDNS0SUBNET, Family: 1, SourceNetmask: 24, Address: net.ParseIP("192.0.2.0").To4()})
	}
	return query
}

func TestDNSCurrentQueryConditionsUseRequestMetadata(t *testing.T) {
	ctx, router, _ := openDNSRuleWitness(t, `[{"query_client_subnet":["192.0.2.0/24"],"query_dnssec":true,"server":"a"}]`, "")
	for _, test := range []struct{ subnet, dnssec bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		query := advancedDNSQuery("query-condition.example.", test.subnet, test.dnssec)
		response, err := router.Exchange(ctx, query, adapter.DNSQueryOptions{})
		if err != nil || len(response.Answer) != 1 {
			t.Fatalf("query-condition exchange failed: %v", err)
		}
		want := "198.51.100.2"
		if test.subnet && test.dnssec {
			want = "192.0.2.1"
		}
		if response.Answer[0].(*mDNS.A).A.String() != want {
			t.Fatalf("query metadata subnet=%t DNSSEC=%t selected wrong rule", test.subnet, test.dnssec)
		}
	}
}

func TestDNSCurrentSubnetRemovalAndPreferredDomainRuntime(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	observed := make(chan *mDNS.Msg, 4)
	started := make(chan struct{})
	server := &mDNS.Server{PacketConn: conn, NotifyStartedFunc: func() { close(started) }, Handler: mDNS.HandlerFunc(func(writer mDNS.ResponseWriter, query *mDNS.Msg) {
		observed <- query.Copy()
		response := new(mDNS.Msg)
		response.SetReply(query)
		response.Answer = []mDNS.RR{&mDNS.A{Hdr: mDNS.RR_Header{Name: query.Question[0].Name, Rrtype: mDNS.TypeA, Class: mDNS.ClassINET, Ttl: 60}, A: net.ParseIP("198.51.100.77").To4()}}
		_ = writer.WriteMsg(response)
	})}
	go func() { _ = server.ActivateAndServe() }()
	t.Cleanup(func() { _ = server.Shutdown() })
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("private DNS witness did not start")
	}
	ctx := registry.Context(context.Background())
	raw := []byte(fmt.Sprintf(`{"log":{"disabled":true},"dns":{"disable_cache":true,"servers":[{"type":"udp","tag":"upstream","server":"127.0.0.1","server_port":%d},{"type":"hosts","tag":"preferred","predefined":{"preferred.example":["192.0.2.90"]}}],"final":"upstream","rules":[{"domain":["remove.example"],"server":"upstream","remove_client_subnet":true},{"preferred_by":["preferred"],"server":"preferred"}]}}`, conn.LocalAddr().(*net.UDPAddr).Port))
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
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	router := sbservice.FromContext[adapter.DNSRouter](ctx)
	for _, domain := range []string{"keep.example.", "remove.example."} {
		if _, err := router.Exchange(bounded, advancedDNSQuery(domain, true, false), adapter.DNSQueryOptions{}); err != nil {
			t.Fatal(err)
		}
		var query *mDNS.Msg
		select {
		case query = <-observed:
		case <-bounded.Done():
			t.Fatal("upstream DNS request was not observed")
		}
		hasSubnet := false
		if edns := query.IsEdns0(); edns != nil {
			for _, value := range edns.Option {
				_, subnet := value.(*mDNS.EDNS0_SUBNET)
				hasSubnet = hasSubnet || subnet
			}
		}
		if hasSubnet != (domain == "keep.example.") {
			t.Fatal("DNS ECS removal changed the wrong request")
		}
	}
	response, err := router.Exchange(bounded, advancedDNSQuery("preferred.example.", false, false), adapter.DNSQueryOptions{})
	if err != nil || len(response.Answer) != 1 || response.Answer[0].(*mDNS.A).A.String() != "192.0.2.90" {
		t.Fatalf("preferred_by did not select its DNS transport: %v", err)
	}
}
