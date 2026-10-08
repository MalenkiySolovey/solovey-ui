package singboxconfig

import (
	"encoding/json"
	"net/netip"
	"net/url"
)

// Supplies actual panel-side HTTP lookups to the DNS owner. Proxy detours
// resolve remotely; direct policies use the panel's DNS router.
func coreHTTPDomainConsumers(root dnsObject) []dnsObject {
	var route dnsObject
	_ = json.Unmarshal(root["route"], &route)
	var sets, clients []dnsObject
	_ = json.Unmarshal(route["rule_set"], &sets)
	_ = json.Unmarshal(root["http_clients"], &clients)
	var consumers []dnsObject
	for _, set := range sets {
		if dnsString(set, "type") != "remote" {
			continue
		}
		address, err := url.Parse(dnsString(set, "url"))
		if err != nil || address.Hostname() == "" {
			continue
		}
		if _, err := netip.ParseAddr(address.Hostname()); err == nil {
			continue
		}
		var client dnsObject
		var tag string
		if activeDNSRaw(set["http_client"]) {
			if json.Unmarshal(set["http_client"], &tag) != nil {
				_ = json.Unmarshal(set["http_client"], &client)
			}
		} else {
			tag = dnsString(route, "default_http_client")
			if tag == "" && len(clients) > 0 {
				tag = dnsString(clients[0], "tag")
			}
			if tag == "" {
				client = dnsObject{"detour": dnsJSON(dnsString(set, "download_detour"))}
				if dnsString(client, "detour") == "" {
					client["detour"] = dnsJSON(defaultOutboundTag(root))
				}
			}
		}
		if tag != "" {
			for _, defined := range clients {
				if dnsString(defined, "tag") == tag {
					client = defined
					break
				}
			}
		}
		resolver := client["domain_resolver"]
		if detour := dnsString(client, "detour"); detour != "" {
			outbound, found := findDNSOutbound(root, detour)
			if !found || dnsString(outbound, "type") != "direct" {
				continue
			}
			if !activeDNSRaw(resolver) {
				resolver = outbound["domain_resolver"]
			}
		}
		consumers = append(consumers, dnsObject{"server": dnsJSON(address.Hostname()), "domain_resolver": resolver})
	}
	return consumers
}
