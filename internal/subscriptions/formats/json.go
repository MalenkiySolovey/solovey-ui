package formats

import (
	"encoding/json"
	"strings"

	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

const defaultJson = `
{
  "inbounds": [
    {
      "type": "tun",
      "address": [
				"172.19.0.1/30",
				"fdfe:dcba:9876::1/126"
			],
      "mtu": 9000,
      "auto_route": true,
      "strict_route": false,
      "endpoint_independent_nat": false,
      "stack": "system",
      "platform": {
        "http_proxy": {
          "enabled": true,
          "server": "127.0.0.1",
          "server_port": 2080
        }
      }
    },
    {
      "type": "mixed",
      "listen": "127.0.0.1",
      "listen_port": 2080,
      "users": []
    }
  ]
}
`

type JSONOptions struct {
	Extension   string
	DirectRules bool
	Mux         bool
	Noises      string
	Fragment    string
}

func RenderJSON(outbounds []map[string]interface{}, options JSONOptions) (string, error) {
	var jsonConfig map[string]interface{}
	if err := json.Unmarshal([]byte(defaultJson), &jsonConfig); err != nil {
		return "", err
	}
	jsonConfig["outbounds"] = jsonRuntimeOutbounds(outbounds)
	if err := ApplyJSONOptions(&jsonConfig, options); err != nil {
		return "", err
	}
	result, err := json.MarshalIndent(jsonConfig, "", "  ")
	if err != nil {
		return "", err
	}
	return string(result), nil
}

func jsonRuntimeOutbounds(outbounds []map[string]interface{}) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(outbounds))
	for _, outbound := range outbounds {
		if outbound == nil {
			continue
		}
		result = append(result, jsonRuntimeOutbound(outbound))
	}
	return result
}

func jsonRuntimeOutbound(outbound map[string]interface{}) map[string]interface{} {
	clone := make(map[string]interface{}, len(outbound))
	for key, value := range outbound {
		clone[key] = value
	}
	delete(clone, subcanonical.MetadataKey)
	if clone["type"] != "failover" {
		return clone
	}
	clone["type"] = "selector"
	delete(clone, "failover")
	members := jsonStringList(clone["outbounds"])
	if len(members) > 0 && strings.TrimSpace(asString(clone["default"])) == "" {
		clone["default"] = members[0]
	}
	if len(members) > 0 && !jsonStringListContains(members, "direct") {
		clone["outbounds"] = append(members, "direct")
	}
	return clone
}

func ApplyJSONOptions(jsonConfig *map[string]interface{}, options JSONOptions) error {
	if err := addFragment(jsonConfig, options.Fragment); err != nil {
		return err
	}
	if err := addNoises(jsonConfig, options.Noises); err != nil {
		return err
	}
	addMux(jsonConfig, options.Mux)

	rules_start := []interface{}{
		map[string]interface{}{
			"action": "sniff",
		},
		map[string]interface{}{
			"clash_mode": "Direct",
			"action":     "route",
			"outbound":   "direct",
		},
	}
	rules_end := []interface{}{
		map[string]interface{}{
			"clash_mode": "Global",
			"action":     "route",
			"outbound":   "proxy",
		},
	}
	route := map[string]interface{}{
		"auto_detect_interface": true,
		"final":                 "proxy",
		"rules":                 rules_start,
	}

	othersStr := options.Extension
	if len(othersStr) == 0 {
		addDirectRules(route, options.DirectRules)
		(*jsonConfig)["route"] = route
		return nil
	}
	var othersJson map[string]interface{}
	if err := json.Unmarshal([]byte(othersStr), &othersJson); err != nil {
		return err
	}
	if _, ok := othersJson["log"]; ok {
		(*jsonConfig)["log"] = othersJson["log"]
	}
	if _, ok := othersJson["dns"]; ok {
		(*jsonConfig)["dns"] = othersJson["dns"]
	}
	if _, ok := othersJson["inbounds"]; ok {
		(*jsonConfig)["inbounds"] = othersJson["inbounds"]
	}
	if _, ok := othersJson["experimental"]; ok {
		(*jsonConfig)["experimental"] = othersJson["experimental"]
	}
	if _, ok := othersJson["rule_set"]; ok {
		route["rule_set"] = othersJson["rule_set"]
	}
	if settingRules, ok := othersJson["rules"].([]interface{}); ok {
		rules := append(rules_start, settingRules...)
		route["rules"] = append(rules, rules_end...)
	}
	if defaultDomainResolver, ok := othersJson["default_domain_resolver"].(string); ok {
		route["default_domain_resolver"] = defaultDomainResolver
	}
	addDirectRules(route, options.DirectRules)
	(*jsonConfig)["route"] = route

	return nil
}

func addDirectRules(route map[string]interface{}, enabled bool) {
	if !enabled {
		return
	}
	route["rule_set"] = mergeDirectRuleSets(route["rule_set"])
	rules, _ := route["rules"].([]interface{})
	route["rules"] = insertDirectRouteRules(rules)
}

func insertDirectRouteRules(rules []interface{}) []interface{} {
	directRule := map[string]interface{}{
		"rule_set": []string{"geosite-private", "geoip-private"},
		"action":   "route",
		"outbound": "direct",
	}
	if len(rules) == 0 {
		return []interface{}{directRule}
	}
	result := make([]interface{}, 0, len(rules)+1)
	result = append(result, rules[0], directRule)
	result = append(result, rules[1:]...)
	return result
}

func mergeDirectRuleSets(existing interface{}) []interface{} {
	result := make([]interface{}, 0)
	seen := map[string]bool{}
	if ruleSets, ok := existing.([]interface{}); ok {
		for _, ruleSet := range ruleSets {
			if tag, ok := ruleSetTag(ruleSet); ok {
				seen[tag] = true
			}
			result = append(result, ruleSet)
		}
	}
	for _, ruleSet := range directRuleSets() {
		tag, _ := ruleSetTag(ruleSet)
		if seen[tag] {
			continue
		}
		seen[tag] = true
		result = append(result, ruleSet)
	}
	return result
}

func ruleSetTag(ruleSet interface{}) (string, bool) {
	ruleSetMap, ok := ruleSet.(map[string]interface{})
	if !ok {
		return "", false
	}
	tag, ok := ruleSetMap["tag"].(string)
	return tag, ok && tag != ""
}

func directRuleSets() []interface{} {
	return []interface{}{
		map[string]interface{}{
			"tag":             "geosite-private",
			"type":            "remote",
			"format":          "binary",
			"url":             "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/private.srs",
			"download_detour": "direct",
		},
		map[string]interface{}{
			"tag":             "geoip-private",
			"type":            "remote",
			"format":          "binary",
			"url":             "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/private.srs",
			"download_detour": "direct",
		},
	}
}

func addMux(jsonConfig *map[string]interface{}, enabled bool) {
	if !enabled {
		return
	}
	outbounds, ok := jsonConfigOutbounds(jsonConfig)
	if !ok {
		return
	}
	for _, outbound := range *outbounds {
		protocol, _ := outbound["type"].(string)
		if supportsJSONMux(protocol) {
			outbound["multiplex"] = map[string]interface{}{
				"enabled":  true,
				"protocol": "smux",
			}
		}
	}
}

func addNoises(jsonConfig *map[string]interface{}, noisesStr string) error {
	if strings.TrimSpace(noisesStr) == "" {
		return nil
	}
	var noises []interface{}
	if err := json.Unmarshal([]byte(noisesStr), &noises); err != nil {
		return err
	}
	outbounds, ok := jsonConfigOutbounds(jsonConfig)
	if !ok {
		return nil
	}
	for _, outbound := range *outbounds {
		protocol, _ := outbound["type"].(string)
		if supportsJSONNoises(protocol) {
			outbound["noises"] = noises
		}
	}
	return nil
}

func addFragment(jsonConfig *map[string]interface{}, fragmentStr string) error {
	if strings.TrimSpace(fragmentStr) == "" {
		return nil
	}
	var fragment map[string]interface{}
	if err := json.Unmarshal([]byte(fragmentStr), &fragment); err != nil {
		return err
	}
	outbounds, ok := jsonConfigOutbounds(jsonConfig)
	if !ok {
		return nil
	}
	for _, outbound := range *outbounds {
		protocol, _ := outbound["type"].(string)
		if supportsJSONFragment(protocol) {
			outbound["fragment"] = fragment
		}
	}
	return nil
}

func jsonConfigOutbounds(jsonConfig *map[string]interface{}) (*[]map[string]interface{}, bool) {
	switch outbounds := (*jsonConfig)["outbounds"].(type) {
	case *[]map[string]interface{}:
		return outbounds, true
	case []map[string]interface{}:
		return &outbounds, true
	default:
		return nil, false
	}
}

func jsonStringList(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []interface{}:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := strings.TrimSpace(asString(item)); value != "" {
				result = append(result, value)
			}
		}
		return result
	default:
		return nil
	}
}

func jsonStringListContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func supportsJSONMux(protocol string) bool {
	switch protocol {
	case "vless", "vmess", "trojan", "shadowsocks":
		return true
	default:
		return false
	}
}

func supportsJSONNoises(protocol string) bool {
	switch protocol {
	case "vless", "vmess", "trojan":
		return true
	default:
		return false
	}
}

func supportsJSONFragment(protocol string) bool {
	switch protocol {
	case "vless", "vmess", "trojan":
		return true
	default:
		return false
	}
}
