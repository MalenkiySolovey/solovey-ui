package box

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/sagernet/sing-box/adapter"
	boxoutbound "github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	sbservice "github.com/sagernet/sing/service"
)

func TestAcceptedHTTPRuleSetPaths(t *testing.T) {
	var report []map[string]any
	for _, test := range []struct {
		name, extra string
		expected    int32
	}{
		{"explicit-direct", `,"download_detour":"direct"`, 0},
		{"explicit-proxy", `,"download_detour":"proxy"`, 1},
		{"implicit-final-proxy", ``, 1},
		{"empty-client-final-proxy", `,"http_client":{}`, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var serverHits, proxyHits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				serverHits.Add(1)
				_, _ = w.Write([]byte(`{"version":3,"rules":[{"domain_suffix":["example.com"]}]}`))
			}))
			defer server.Close()
			ctx := registry.Context(context.Background())
			outboundRegistry := registry.OutboundRegistry()
			boxoutbound.Register[struct{}](outboundRegistry, "witness", func(_ context.Context, _ adapter.Router, _ log.ContextLogger, tag string, _ struct{}) (adapter.Outbound, error) {
				return &dnsPathWitness{Adapter: boxoutbound.NewAdapter("witness", tag, []string{"tcp", "udp"}, nil), hits: &proxyHits}, nil
			})
			ctx = sbservice.ContextWith[option.OutboundOptionsRegistry](ctx, outboundRegistry)
			ctx = sbservice.ContextWith[adapter.OutboundRegistry](ctx, outboundRegistry)
			raw := []byte(fmt.Sprintf(`{"log":{"disabled":true},"route":{"final":"proxy","rule_set":[{"tag":"test","type":"remote","format":"source","url":%q,"update_interval":"1h"%s}],"rules":[{"rule_set":"test","outbound":"direct"}]},"outbounds":[{"type":"direct","tag":"direct"},{"type":"witness","tag":"proxy"}]}`, server.URL+"/custom.json", test.extra))
			prepared, err := singboxconfig.PrepareHTTPUpgrade(raw)
			if err != nil {
				t.Fatal(err)
			}
			again, err := singboxconfig.PrepareHTTPUpgrade(prepared.Candidate)
			if err != nil || string(again.Candidate) != string(prepared.Candidate) {
				t.Fatal("download projection not idempotent")
			}
			raw = prepared.Candidate
			var options option.Options
			if err := options.UnmarshalJSONContext(ctx, raw); err != nil {
				t.Fatal(err)
			}
			instance, err := NewBox(Options{Context: ctx, Options: options})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if err := instance.Start(); err != nil {
				t.Fatal(err)
			}
			if proxyHits.Load() != test.expected || serverHits.Load() != 1 {
				t.Fatalf("path witness proxy=%d expected=%d server=%d", proxyHits.Load(), test.expected, serverHits.Load())
			}
			report = append(report, map[string]any{"case": test.name, "proxy_path_calls": proxyHits.Load(), "download_calls": serverHits.Load(), "migration_applied": true, "idempotence_verified": true})
		})
	}
	captureHTTPReport(t, "http-path-behavior.json", report)
}

func TestAcceptedRouteRuleSetGroups(t *testing.T) {
	directory := os.Getenv("SOLOVEY_HTTP_FIXTURES")
	if directory == "" {
		directory = filepath.Join("..", "..", "internal", "singbox", "config", "testdata", "accepted-1.13-http")
	}
	var report []map[string]any
	for _, id := range []string{"flat-grouped-set", "multiple-grouped-set", "inverted-grouped-set", "logical-grouped-set"} {
		t.Run(id, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(directory, id+".json"))
			if err != nil {
				t.Fatal(err)
			}
			prepared, upgradeErr := singboxconfig.PrepareHTTPUpgrade(raw)
			if id == "flat-grouped-set" {
				if upgradeErr != nil {
					t.Fatal(upgradeErr)
				}
				raw = prepared.Candidate
			} else if upgradeErr == nil || string(prepared.Candidate) != string(raw) {
				t.Fatal("changed grouped semantics must require an explicit decision and preserve preimage")
			}
			ctx := registry.Context(context.Background())
			var options option.Options
			if err := options.UnmarshalJSONContext(ctx, raw); err != nil {
				t.Fatal(err)
			}
			instance, err := NewBox(Options{Context: ctx, Options: options})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			if err := instance.Start(); err != nil {
				t.Fatal(err)
			}
			for _, domain := range []string{"www.example", "www.other"} {
				for _, source := range []string{"192.0.2.1:1000", "198.51.100.1:1000"} {
					for _, destination := range []string{"192.0.2.2:443", "203.0.113.1:443"} {
						metadata := adapter.InboundContext{Domain: domain, Network: "tcp", Source: M.ParseSocksaddr(source), Destination: M.ParseSocksaddr(destination)}
						matched := sbservice.FromContext[adapter.Router](ctx).Rules()[0].Match(&metadata)
						expected := id == "flat-grouped-set" || (id == "multiple-grouped-set" && domain == "www.example" && destination == "192.0.2.2:443") || (id == "inverted-grouped-set" && source == "192.0.2.1:1000")
						if matched != expected {
							t.Fatalf("pinned grouped behavior changed: %s %s %s %s", id, domain, source, destination)
						}
						report = append(report, map[string]any{"case": id, "domain": domain, "source": source, "destination": destination, "matched": matched})
					}
				}
			}
		})
	}
	captureHTTPReport(t, "ruleset-group-behavior.json", report)
}

func captureHTTPReport(t *testing.T, name string, value any) {
	t.Helper()
	if directory := os.Getenv("SOLOVEY_HTTP_BEHAVIOR_CAPTURE"); directory != "" {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPinnedHTTPRuleSetCacheAndTags(t *testing.T) {
	var hits atomic.Int32
	var unavailable atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if unavailable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		suffix := "alpha.example"
		if r.URL.Path == "/beta.json" {
			suffix = "beta.example"
		}
		_, _ = fmt.Fprintf(w, `{"version":3,"rules":[{"domain_suffix":[%q]}]}`, suffix)
	}))
	defer server.Close()
	cachePath := filepath.Join(t.TempDir(), "rule-cache.db")
	open := func(path string) (*Box, adapter.RuleSet, error) {
		ctx := registry.Context(context.Background())
		raw := []byte(fmt.Sprintf(`{"log":{"disabled":true},"http_clients":[{"tag":"direct-http","engine":"go","version":2}],"route":{"final":"direct","default_http_client":"direct-http","rule_set":[{"tag":"cached","type":"remote","format":"source","url":%q,"http_client":"direct-http","update_interval":"1h"}],"rules":[{"rule_set":"cached","outbound":"direct"}]},"outbounds":[{"type":"direct","tag":"direct"}],"experimental":{"cache_file":{"enabled":true,"path":%q}}}`, server.URL+path, cachePath))
		var options option.Options
		if err := options.UnmarshalJSONContext(ctx, raw); err != nil {
			return nil, nil, err
		}
		instance, err := NewBox(Options{Context: ctx, Options: options})
		if err != nil {
			return nil, nil, err
		}
		if err := instance.Start(); err != nil {
			_ = instance.Close()
			return nil, nil, err
		}
		ruleSet, _ := sbservice.FromContext[adapter.Router](ctx).RuleSet("cached")
		return instance, ruleSet, nil
	}
	match := func(set adapter.RuleSet, domain string) bool {
		metadata := adapter.InboundContext{Domain: domain, Network: "tcp", Destination: M.ParseSocksaddr("203.0.113.1:443")}
		return set.Match(&metadata)
	}
	first, set, err := open("/alpha.json")
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 || !match(set, "www.alpha.example") {
		t.Fatal("initial URL witness failed")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, set, err := open("/alpha.json")
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 || !match(set, "www.alpha.example") {
		t.Fatal("cache reuse failed")
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	third, set, err := open("/beta.json")
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 || !match(set, "www.beta.example") || match(set, "www.alpha.example") {
		t.Fatal("changed URL must invalidate cached contents")
	}
	if err := third.Close(); err != nil {
		t.Fatal(err)
	}
	unavailable.Store(true)
	fourth, set, err := open("/beta.json")
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 || !match(set, "www.beta.example") {
		t.Fatal("fresh owned cache fallback failed")
	}
	if err := fourth.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := open("/other.json"); err == nil {
		t.Fatal("different URL must not activate stale cached content")
	}
	captureHTTPReport(t, "cache-url-behavior.json", map[string]any{"same_url_reuses_cache": true, "changed_url_refetched": true, "update_interval": "1h", "fresh_cache_server_unavailable": true, "changed_url_server_unavailable_blocks": true})
	unavailable.Store(false)
	ctx := registry.Context(context.Background())
	raw := []byte(fmt.Sprintf(`{"log":{"disabled":true},"http_clients":[{"tag":"direct-http","engine":"go","version":2}],"route":{"default_http_client":"direct-http","rule_set":[{"tag":["alpha","beta"],"type":"remote","format":"source","url":%q,"http_client":"direct-http"}],"rules":[{"rule_set":["alpha","beta"],"outbound":"direct"}]},"outbounds":[{"type":"direct","tag":"direct"}]}`, server.URL+"/{tag}.json"))
	var options option.Options
	if err := options.UnmarshalJSONContext(ctx, raw); err != nil {
		t.Fatal(err)
	}
	before := hits.Load()
	instance, err := NewBox(Options{Context: ctx, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	if err := instance.Start(); err != nil {
		t.Fatal(err)
	}
	router := sbservice.FromContext[adapter.Router](ctx)
	alpha, ok := router.RuleSet("alpha")
	if !ok || !match(alpha, "www.alpha.example") {
		t.Fatal("first tag contents lost")
	}
	beta, ok := router.RuleSet("beta")
	if !ok || !match(beta, "www.beta.example") || match(beta, "www.alpha.example") {
		t.Fatal("second tag contents mixed")
	}
	if hits.Load()-before != 2 {
		t.Fatal("multi tag must fetch each substituted identity once")
	}
	captureHTTPReport(t, "multi-tag-behavior.json", map[string]any{"tags": []string{"alpha", "beta"}, "separate_contents": true, "downloads": 2})
}
