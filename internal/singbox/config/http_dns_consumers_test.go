package singboxconfig

import (
	"encoding/json"
	"testing"
)

func TestNativeACMEHTTPConsumerUsesItsOwnPolicy(t *testing.T) {
	var root dnsObject
	if err := json.Unmarshal([]byte(`{"certificate_providers":[{"type":"acme","tag":"native","provider":"https://ca.fixture.invalid/directory","dns01_challenge":{"provider":"cloudflare"}}],"http_clients":[{"tag":"proxy-http","detour":"proxy"},{"tag":"local-http","domain_resolver":"chosen"}],"outbounds":[{"type":"socks","tag":"proxy","server":"127.0.0.1","server_port":1080}],"route":{"default_http_client":"proxy-http"}}`), &root); err != nil {
		t.Fatal(err)
	}
	consumers := coreHTTPDomainConsumers(root)
	if len(consumers) != 2 || dnsString(consumers[0], "server") != "ca.fixture.invalid" || activeDNSRaw(consumers[0]["domain_resolver"]) {
		t.Fatal("native absent client inherited route or shared default")
	}
	var providers []dnsObject
	_ = json.Unmarshal(root["certificate_providers"], &providers)
	providers[0]["http_client"] = dnsJSON("local-http")
	root["certificate_providers"] = dnsJSON(providers)
	consumers = coreHTTPDomainConsumers(root)
	if len(consumers) != 2 || dnsString(consumers[0], "domain_resolver") != "chosen" || dnsString(consumers[1], "domain_resolver") != "chosen" {
		t.Fatal("native consumers lost their shared resolver")
	}
	providers[0]["http_client"] = dnsJSON("proxy-http")
	root["certificate_providers"] = dnsJSON(providers)
	if len(coreHTTPDomainConsumers(root)) != 0 {
		t.Fatal("proxy ACME was treated as a panel-local DNS lookup")
	}
	providers[0]["http_client"] = dnsJSON("missing")
	root["certificate_providers"] = dnsJSON(providers)
	raw, _ := json.Marshal(root)
	if _, err := ValidateHTTPConfig(raw); err == nil {
		t.Fatal("missing native provider HTTP reference accepted")
	}
	delete(providers[0], "http_client")
	providers[0]["dns01_challenge"] = dnsJSON(map[string]string{"provider": "alidns"})
	root["certificate_providers"] = dnsJSON(providers)
	if len(coreHTTPDomainConsumers(root)) != 1 {
		t.Fatal("AliDNS system HTTP was incorrectly assigned to core HTTP")
	}
}
