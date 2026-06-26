package doctor

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func ReferenceChecks(rawConfig []byte) []Item {
	var cfg struct {
		DNS struct {
			Final   string           `json:"final"`
			Servers []map[string]any `json:"servers"`
			Rules   []map[string]any `json:"rules"`
		} `json:"dns"`
		Route struct {
			Final   string           `json:"final"`
			Rules   []map[string]any `json:"rules"`
			RuleSet []map[string]any `json:"rule_set"`
		} `json:"route"`
		Outbounds []map[string]any `json:"outbounds"`
		Endpoints []map[string]any `json:"endpoints"`
	}
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return []Item{Error("reference-parse", "Reference scan", err.Error(), "Fix malformed config JSON.", nil)}
	}

	dnsTags := map[string]bool{}
	for i, server := range cfg.DNS.Servers {
		tag := stringField(server, "tag")
		if tag == "" {
			tag = strconv.Itoa(i)
		}
		dnsTags[tag] = true
	}
	outboundTags := map[string]bool{"direct": true, "block": true, "dns-out": true, "dns": true}
	for _, outbound := range cfg.Outbounds {
		if tag := stringField(outbound, "tag"); tag != "" {
			outboundTags[tag] = true
		}
	}
	for _, endpoint := range cfg.Endpoints {
		if tag := stringField(endpoint, "tag"); tag != "" {
			outboundTags[tag] = true
		}
	}

	var items []Item
	var missingDNS []string
	if cfg.DNS.Final != "" && !dnsTags[cfg.DNS.Final] {
		missingDNS = append(missingDNS, "dns.final -> "+cfg.DNS.Final)
	}
	for i, rule := range cfg.DNS.Rules {
		if server := stringField(rule, "server"); server != "" && !dnsTags[server] {
			missingDNS = append(missingDNS, fmt.Sprintf("dns.rules[%d].server -> %s", i, server))
		}
	}
	if len(missingDNS) > 0 {
		items = append(items, Error("dns-references", "DNS references", "DNS references missing server tags.", "Create the missing DNS server or update rules/final DNS.", missingDNS))
	} else {
		items = append(items, OK("dns-references", "DNS references", "DNS final and rule server references resolve.", nil))
	}

	var missingOutbounds []string
	if cfg.Route.Final != "" && !outboundTags[cfg.Route.Final] {
		missingOutbounds = append(missingOutbounds, "route.final -> "+cfg.Route.Final)
	}
	for i, rule := range cfg.Route.Rules {
		missingOutbounds = appendMissingRouteOutbound(missingOutbounds, rule, i, outboundTags)
	}
	if len(missingOutbounds) > 0 {
		items = append(items, Error("route-references", "Route references", "Route references missing outbound/endpoint tags.", "Create the missing outbound or update the route rule/final outbound.", missingOutbounds))
	} else {
		items = append(items, OK("route-references", "Route references", "Route final and rule outbound references resolve.", nil))
	}

	items = append(items, RuleSetURLChecks(cfg.Route.RuleSet)...)
	return items
}

func RuleSetURLChecks(ruleSets []map[string]any) []Item {
	var invalid []string
	for i, ruleSet := range ruleSets {
		if stringField(ruleSet, "type") != "remote" {
			continue
		}
		tag := stringField(ruleSet, "tag")
		rawURL := stringField(ruleSet, "url")
		if rawURL == "" {
			invalid = append(invalid, fmt.Sprintf("rule_set[%d] %q has empty url", i, tag))
			continue
		}
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			invalid = append(invalid, fmt.Sprintf("rule_set[%d] %q has invalid url", i, tag))
			continue
		}
		if format := stringField(ruleSet, "format"); strings.HasSuffix(parsed.Path, ".srs") && format != "" && format != "binary" {
			invalid = append(invalid, fmt.Sprintf("rule_set[%d] %q uses .srs with non-binary format", i, tag))
		}
	}
	if len(invalid) > 0 {
		return []Item{Warn("ruleset-urls", "Remote rule-set URLs", "Some remote rule-set URLs look unsafe or inconsistent.", "Use HTTPS raw URLs without credentials and binary format for .srs files.", invalid)}
	}
	return []Item{OK("ruleset-urls", "Remote rule-set URLs", "Remote rule-set URLs have a safe shape.", nil)}
}

func appendMissingRouteOutbound(missing []string, rule map[string]any, index int, tags map[string]bool) []string {
	if outbound := stringField(rule, "outbound"); outbound != "" && !tags[outbound] {
		missing = append(missing, fmt.Sprintf("route.rules[%d].outbound -> %s", index, outbound))
	}
	if nested, ok := rule["rules"].([]any); ok {
		for _, item := range nested {
			if sub, ok := item.(map[string]any); ok {
				missing = appendMissingRouteOutbound(missing, sub, index, tags)
			}
		}
	}
	return missing
}

func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
