package formats

import (
	"encoding/json"
	"testing"

	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

func TestRenderXrayMapsSingBoxOutboundsAndGroups(t *testing.T) {
	rendered, err := RenderXray([]map[string]interface{}{
		{
			"type":        "vless",
			"tag":         "proxy-a",
			"server":      "edge.example.com",
			"server_port": 443,
			"uuid":        "11111111-1111-1111-1111-111111111111",
			"flow":        "xtls-rprx-vision",
			"tls": map[string]interface{}{
				"enabled":     true,
				"server_name": "sni.example.com",
				"reality": map[string]interface{}{
					"enabled":    true,
					"public_key": "pub",
					"short_id":   "sid",
				},
				"utls": map[string]interface{}{"fingerprint": "chrome"},
			},
			"transport": map[string]interface{}{
				"type": "ws",
				"path": "/ws",
				"headers": map[string]interface{}{
					"Host": "cdn.example.com",
				},
			},
		},
		{
			"type":      "urltest",
			"tag":       "auto",
			"outbounds": []string{"proxy-a"},
			"default":   "proxy-a",
			subcanonical.MetadataKey: []subcanonical.Adaptation{
				{
					SourceFormat:  subcanonical.FormatXray,
					SourceFeature: "routing.balancer",
					SourceType:    "balancer",
					TargetType:    "urltest",
					Strategy:      "leastLoad",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(rendered), &config); err != nil {
		t.Fatal(err)
	}
	outbounds, _ := config["outbounds"].([]interface{})
	if len(outbounds) != 3 {
		t.Fatalf("outbounds = %#v", outbounds)
	}
	proxy, _ := outbounds[0].(map[string]interface{})
	if proxy["protocol"] != "vless" || proxy["tag"] != "proxy-a" {
		t.Fatalf("proxy = %#v", proxy)
	}
	stream, _ := proxy["streamSettings"].(map[string]interface{})
	reality, _ := stream["realitySettings"].(map[string]interface{})
	if stream["security"] != "reality" || reality["publicKey"] != "pub" {
		t.Fatalf("stream = %#v", stream)
	}
	routing, _ := config["routing"].(map[string]interface{})
	balancers, _ := routing["balancers"].([]interface{})
	if len(balancers) != 1 {
		t.Fatalf("balancers = %#v", routing["balancers"])
	}
	balancer, _ := balancers[0].(map[string]interface{})
	if balancer["tag"] != "auto" {
		t.Fatalf("balancer = %#v", balancer)
	}
	selector, _ := balancer["selector"].([]interface{})
	if len(selector) != 1 || selector[0] != "proxy-a" {
		t.Fatalf("selector = %#v", selector)
	}
	strategy, _ := balancer["strategy"].(map[string]interface{})
	if strategy["type"] != "leastLoad" || balancer["fallbackTag"] != "proxy-a" {
		t.Fatalf("balancer strategy/fallback = %#v", balancer)
	}
}

func TestRenderXrayCreatesDefaultBalancer(t *testing.T) {
	rendered, err := RenderXray([]map[string]interface{}{
		{
			"type":        "trojan",
			"tag":         "node",
			"server":      "trojan.example.com",
			"server_port": 443,
			"password":    "secret",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(rendered), &config); err != nil {
		t.Fatal(err)
	}
	routing, _ := config["routing"].(map[string]interface{})
	balancers, _ := routing["balancers"].([]interface{})
	if len(balancers) != 1 {
		t.Fatalf("balancers = %#v", routing["balancers"])
	}
	balancer, _ := balancers[0].(map[string]interface{})
	if balancer["tag"] != "proxy" {
		t.Fatalf("default balancer = %#v", balancer)
	}
}
