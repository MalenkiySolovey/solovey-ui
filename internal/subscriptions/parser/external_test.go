package parser

import (
	"reflect"
	"strings"
	"testing"
)

func TestNativeGroupInlinePeersRequireExplicitMapping(t *testing.T) {
	for _, body := range []string{
		`{"outbounds":[{"type":"failover","tag":"native","outbounds":[{"type":"socks","server":"private-peer-value","server_port":1080}]}]}`,
		`{"outbounds":[{"type":"bond","tag":"native","outbounds":[{"outbound":{"type":"selector","outbounds":["a","b"]},"download_ratio":100,"upload_ratio":100}]}]}`,
		`{"outbounds":[{"type":"failover","outbounds":["leaf",{"type":"selector","outbounds":["leaf"]}]}]}`,
	} {
		rows, err := ParseSingBoxOutbounds(body)
		if err == nil || rows != nil || strings.Contains(err.Error(), "private-peer-value") {
			t.Fatal("inline native peer was silently flattened or exposed")
		}
	}
}

func TestParseExternalOutboundsKeepsSingBoxGroupsAndFiltersUtilityOutbounds(t *testing.T) {
	result, err := ParseExternalOutbounds(`{
		"outbounds": [
			{"type":"direct","tag":"direct"},
			{"type":"selector","tag":"select","outbounds":["node-a","node-b"]},
			{"type":"urltest","tag":"auto","outbounds":["node-a","node-b"]},
			{"type":"block","tag":"block"},
			{"type":"dns","tag":"dns-out"},
			{"type":"vless","tag":"node-a"},
			{"type":"trojan","tag":"node-b"}
		]
	}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{result[0]["tag"].(string), result[1]["tag"].(string), result[2]["tag"].(string), result[3]["tag"].(string)}
	if !reflect.DeepEqual(got, []string{"select", "auto", "node-a", "node-b"}) {
		t.Fatalf("tags = %#v", got)
	}
}

func TestParseExternalOutboundsUsesLinkParser(t *testing.T) {
	var parsed []string
	result, err := ParseExternalOutbounds("link-a\n\n link-b ", func(link string, index int) (*map[string]interface{}, string, error) {
		if index != 0 {
			t.Fatalf("compatibility index = %d, want 0", index)
		}
		parsed = append(parsed, link)
		outbound := map[string]interface{}{"type": "vless", "tag": link}
		return &outbound, link, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed, []string{"link-a", "link-b"}) {
		t.Fatalf("parsed links = %#v", parsed)
	}
	if result[1]["tag"] != "link-b" {
		t.Fatalf("second tag = %#v", result[1]["tag"])
	}
}

func TestParseExternalOutboundsRejectsMissingOutbounds(t *testing.T) {
	_, err := ParseExternalOutbounds(`{"dns":{}}`, nil)
	if err == nil || !strings.Contains(err.Error(), "missing outbounds") {
		t.Fatalf("expected missing outbounds error, got %v", err)
	}
}

func TestParseSingBoxOutboundsIgnoresXrayStyleOutbounds(t *testing.T) {
	_, err := ParseSingBoxOutbounds(`{
		"outbounds": [
			{"protocol":"vless","tag":"node-a"}
		]
	}`)
	if err == nil || !strings.Contains(err.Error(), "no result") {
		t.Fatalf("expected no result for xray-style outbounds, got %v", err)
	}
}
