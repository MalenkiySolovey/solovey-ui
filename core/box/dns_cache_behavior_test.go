package box

import (
	"context"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	mDNS "github.com/miekg/dns"
	"github.com/sagernet/sing-box/adapter"
	boxdns "github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport/mdns"
	"github.com/sagernet/sing-box/experimental/cachefile"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

type dnsCacheWitness struct {
	tag, address string
	calls        int
}

func (t *dnsCacheWitness) Type() string                   { return "udp" }
func (t *dnsCacheWitness) Tag() string                    { return t.tag }
func (t *dnsCacheWitness) Dependencies() []string         { return nil }
func (t *dnsCacheWitness) Start(adapter.StartStage) error { return nil }
func (t *dnsCacheWitness) Close() error                   { return nil }
func (t *dnsCacheWitness) Reset()                         {}
func (t *dnsCacheWitness) Exchange(_ context.Context, q *mDNS.Msg) (*mDNS.Msg, error) {
	t.calls++
	r := new(mDNS.Msg)
	r.SetReply(q)
	r.Answer = []mDNS.RR{&mDNS.A{Hdr: mDNS.RR_Header{Name: q.Question[0].Name, Rrtype: mDNS.TypeA, Class: mDNS.ClassINET, Ttl: 60}, A: net.ParseIP(t.address)}}
	return r, nil
}
func (t *dnsCacheWitness) ExchangeAsync(ctx context.Context, q *mDNS.Msg, cb func(*mDNS.Msg, error)) {
	r, e := t.Exchange(ctx, q)
	cb(r, e)
}

func TestDNSUpgradeCacheAlwaysPartitionsByTransport(t *testing.T) {
	client := boxdns.NewClient(boxdns.ClientOptions{Context: context.Background(), Logger: log.NewNOPFactory().Logger()})
	client.Start()
	a := &dnsCacheWitness{tag: "a", address: "192.0.2.1"}
	b := &dnsCacheWitness{tag: "b", address: "192.0.2.2"}
	query := new(mDNS.Msg)
	query.SetQuestion("cache.example.", mDNS.TypeA)
	for _, transport := range []*dnsCacheWitness{a, b, a, b} {
		response, err := client.Exchange(context.Background(), transport, query, adapter.DNSQueryOptions{}, nil)
		if err != nil || response.Answer[0].(*mDNS.A).A.String() != transport.address {
			t.Fatal("cache crossed transport identities")
		}
	}
	if a.calls != 1 || b.calls != 1 {
		t.Fatal("independent caches did not reuse their own entries")
	}
}

func TestDNSUpgradeCachePrivacyRestartAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	ctx := registry.Context(context.Background())
	open := func(options option.CacheFileOptions) *cachefile.CacheFile {
		options.Path = path
		cache := cachefile.New(ctx, log.NewNOPFactory().Logger(), options)
		if err := cache.Start(adapter.StartStateInitialize); err != nil {
			t.Fatal(err)
		}
		return cache
	}
	options := option.CacheFileOptions{Enabled: true, StoreRDRC: true, StoreFakeIP: true}
	cache := open(options)
	if !cache.StoreRDRC() || cache.StoreDNS() {
		t.Fatal("rejection-only persistence widened")
	}
	if err := cache.SaveRDRC("a", "reject.example.", mDNS.TypeA); err != nil {
		t.Fatal(err)
	}
	address := netip.MustParseAddr("198.18.0.1")
	if err := cache.FakeIPStore(address, "fake.example"); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	cache = open(options)
	if !cache.LoadRDRC("a", "reject.example.", mDNS.TypeA) || cache.LoadRDRC("b", "reject.example.", mDNS.TypeA) {
		t.Fatal("rejection identity lost across restart")
	}
	if domain, loaded := cache.FakeIPLoad(address); !loaded || domain != "fake.example" {
		t.Fatal("FakeIP bucket lost")
	}
	if err := cache.FakeIPReset(); err != nil {
		t.Fatal(err)
	}
	if _, loaded := cache.FakeIPLoad(address); loaded {
		t.Fatal("FakeIP reset retained mapping")
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	// Full persistence is a separate explicit policy. It must survive restart
	// without pretending the old binary can read this new cache representation.
	full := option.CacheFileOptions{Enabled: true, StoreDNS: true}
	cache = open(full)
	query := new(mDNS.Msg)
	query.SetQuestion("full.example.", mDNS.TypeA)
	response := new(mDNS.Msg)
	response.SetReply(query)
	packed, _ := response.Pack()
	if err := cache.SaveDNSCache("a", "full.example.", mDNS.TypeA, packed, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_ = cache.Close()
	cache = open(full)
	if _, _, loaded := cache.LoadDNSCache("a", "full.example.", mDNS.TypeA); !loaded {
		t.Fatal("explicit full cache lost")
	}
	if _, _, loaded := cache.LoadDNSCache("b", "full.example.", mDNS.TypeA); loaded {
		t.Fatal("full cache crossed transport")
	}
	_ = cache.Close()
	if err := os.WriteFile(path, make([]byte, 8192), 0600); err != nil {
		t.Fatal(err)
	}
	cache = open(full)
	defer cache.Close()
	if _, _, loaded := cache.LoadDNSCache("a", "full.example.", mDNS.TypeA); loaded {
		t.Fatal("corrupt cache resurrected response")
	}
}

func TestDNSUpgradeLocalAndLinkLocalClassification(t *testing.T) {
	for _, name := range []string{"printer.local.", "local.", "1.0.254.169.in-addr.arpa.", "0.8.e.f.ip6.arpa."} {
		if !mdns.IsLocalDomain(name) {
			t.Fatal("local or link-local zone not dispatched to multicast")
		}
	}
	for _, name := range []string{"example.com.", "1.2.0.192.in-addr.arpa."} {
		if mdns.IsLocalDomain(name) {
			t.Fatal("unicast zone classified as multicast")
		}
	}
}
