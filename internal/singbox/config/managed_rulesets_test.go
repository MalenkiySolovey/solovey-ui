package singboxconfig

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func TestNormalizeBaseConfigNormalizesManagedRuSmartForStorage(t *testing.T) {
	normalized, err := NormalizeBaseConfig(json.RawMessage(`{
  "dns": {"servers": [], "rules": []},
  "route": {
    "rules": [],
    "rule_set": [{
      "tag": "preset-ru-direct-geosite",
      "type": "remote",
      "format": "source",
      "url": "https://example.invalid/geosite.dat",
      "download_detour": "proxy",
      "update_interval": "1h",
      "path": "/tmp/absolute.srs"
    }]
  }
}`))
	if err != nil {
		t.Fatal(err)
	}

	ruleSet := firstRuleSetByTag(t, []byte(normalized), managedRuSmartRuleSetTag)
	if got := ruleSet["type"]; got != "local" {
		t.Fatalf("type = %v, want local", got)
	}
	if got := ruleSet["format"]; got != "binary" {
		t.Fatalf("format = %v, want binary", got)
	}
	if got := ruleSet["path"]; got != managedRuSmartRuleSetRelativePath {
		t.Fatalf("path = %v, want %q", got, managedRuSmartRuleSetRelativePath)
	}
	for _, key := range []string{"url", "download_detour", "update_interval"} {
		if _, ok := ruleSet[key]; ok {
			t.Fatalf("%s should be stripped from managed local rule-set: %#v", key, ruleSet)
		}
	}
}

func TestBuildRuntimeConfigPreparesAndRewritesManagedRuSmartForRuntime(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dataDir)
	restoreDownloader := setGeositeRuSmartDownloaderForTest(func(ctx context.Context, sourceURL string) ([]byte, error) {
		return testGeositeDat(), nil
	})
	defer restoreDownloader()

	built, err := BuildRuntimeConfig(json.RawMessage(`{
  "dns": {"servers": [], "rules": []},
  "route": {
    "rules": [],
    "rule_set": [{
      "tag": "preset-ru-direct-geosite",
      "type": "local",
      "format": "binary",
      "path": "rulesets/geosite-ru-smart/direct-ru.srs"
    }]
  }
}`), RuntimeSections{
		Inbounds:  []map[string]any{},
		Outbounds: []map[string]any{},
		Services:  []map[string]any{},
		Endpoints: []map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}

	wantPath := filepath.Join(dataDir, filepath.FromSlash(managedRuSmartRuleSetRelativePath))
	ruleSet := firstRuleSetByTag(t, built, managedRuSmartRuleSetTag)
	if got := filepath.Clean(ruleSet["path"].(string)); got != filepath.Clean(wantPath) {
		t.Fatalf("runtime path = %q, want %q", got, wantPath)
	}
	if _, ok := ruleSet["url"]; ok {
		t.Fatalf("runtime managed local rule-set should not contain url: %#v", ruleSet)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("compiled srs was not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(wantPath), managedRuSmartDatFilename)); err != nil {
		t.Fatalf("source geosite.dat was not written: %v", err)
	}
}

func firstRuleSetByTag(t *testing.T, config []byte, tag string) map[string]any {
	t.Helper()
	var top struct {
		Route struct {
			RuleSet []map[string]any `json:"rule_set"`
		} `json:"route"`
	}
	if err := json.Unmarshal(config, &top); err != nil {
		t.Fatal(err)
	}
	for _, item := range top.Route.RuleSet {
		if item["tag"] == tag {
			return item
		}
	}
	t.Fatalf("rule_set %q not found in %s", tag, string(config))
	return nil
}

func testGeositeDat() []byte {
	domain := protowire.AppendTag(nil, 1, protowire.VarintType)
	domain = protowire.AppendVarint(domain, uint64(v2rayDomainTypeDomain))
	domain = protowire.AppendTag(domain, 2, protowire.BytesType)
	domain = protowire.AppendString(domain, "example.ru")

	site := protowire.AppendTag(nil, 1, protowire.BytesType)
	site = protowire.AppendString(site, managedRuSmartCategory)
	site = protowire.AppendTag(site, 2, protowire.BytesType)
	site = protowire.AppendBytes(site, domain)

	list := protowire.AppendTag(nil, 1, protowire.BytesType)
	list = protowire.AppendBytes(list, site)
	return list
}
