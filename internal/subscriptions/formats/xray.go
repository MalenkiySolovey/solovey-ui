package formats

import (
	"encoding/json"
	"strconv"
	"strings"
)

const xrayProbeURL = "http://www.gstatic.com/generate_204"

func RenderXray(outbounds []map[string]interface{}) (string, error) {
	configOutbounds := make([]map[string]interface{}, 0, len(outbounds)+2)
	proxyTags := make([]string, 0, len(outbounds))
	groupOutbounds := make([]map[string]interface{}, 0)

	for _, outbound := range outbounds {
		outboundType := strings.TrimSpace(asString(outbound["type"]))
		switch outboundType {
		case "selector", "urltest", "failover":
			groupOutbounds = append(groupOutbounds, outbound)
		case "direct", "block":
			continue
		default:
			xrayOutbound := xrayProxyOutbound(outbound)
			if len(xrayOutbound) == 0 {
				continue
			}
			configOutbounds = append(configOutbounds, xrayOutbound)
			if tag := strings.TrimSpace(asString(xrayOutbound["tag"])); tag != "" {
				proxyTags = append(proxyTags, tag)
			}
		}
	}

	configOutbounds = append(configOutbounds,
		map[string]interface{}{
			"tag":      "direct",
			"protocol": "freedom",
		},
		map[string]interface{}{
			"tag":      "block",
			"protocol": "blackhole",
		},
	)

	route := map[string]interface{}{
		"domainStrategy": "AsIs",
	}
	if balancers := xrayBalancers(groupOutbounds, proxyTags); len(balancers) > 0 {
		route["balancers"] = balancers
		route["rules"] = []map[string]interface{}{
			{
				"type":        "field",
				"inboundTag":  []string{"socks-in", "http-in"},
				"balancerTag": balancers[0]["tag"],
			},
		}
	}

	config := map[string]interface{}{
		"log": map[string]interface{}{
			"loglevel": "warning",
		},
		"inbounds": []map[string]interface{}{
			{
				"tag":      "socks-in",
				"protocol": "socks",
				"listen":   "127.0.0.1",
				"port":     10808,
				"settings": map[string]interface{}{
					"udp": true,
				},
			},
			{
				"tag":      "http-in",
				"protocol": "http",
				"listen":   "127.0.0.1",
				"port":     10809,
			},
		},
		"outbounds": configOutbounds,
		"routing":   route,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
func xrayProxyOutbound(outbound map[string]interface{}) map[string]interface{} {
	protocol := strings.TrimSpace(asString(outbound["type"]))
	tag := strings.TrimSpace(asString(outbound["tag"]))
	server := strings.TrimSpace(asString(outbound["server"]))
	if tag == "" || protocol == "" || server == "" {
		return nil
	}

	xrayOutbound := map[string]interface{}{
		"tag":      tag,
		"protocol": xrayProtocol(protocol),
	}
	switch protocol {
	case "vless", "vmess":
		user := map[string]interface{}{}
		if uuid := strings.TrimSpace(asString(outbound["uuid"])); uuid != "" {
			user["id"] = uuid
		}
		if flow := strings.TrimSpace(asString(outbound["flow"])); flow != "" {
			user["flow"] = flow
		}
		if protocol == "vless" {
			user["encryption"] = "none"
		} else {
			user["alterId"] = xrayAlterID(outbound["alter_id"])
			user["security"] = xrayStringDefault(outbound["security"], "auto")
		}
		xrayOutbound["settings"] = map[string]interface{}{
			"vnext": []map[string]interface{}{
				{
					"address": server,
					"port":    xrayPort(outbound["server_port"]),
					"users":   []map[string]interface{}{user},
				},
			},
		}
	case "trojan":
		serverConfig := map[string]interface{}{
			"address":  server,
			"port":     xrayPort(outbound["server_port"]),
			"password": asString(outbound["password"]),
		}
		if flow := strings.TrimSpace(asString(outbound["flow"])); flow != "" {
			serverConfig["flow"] = flow
		}
		xrayOutbound["settings"] = map[string]interface{}{"servers": []map[string]interface{}{serverConfig}}
	case "shadowsocks":
		xrayOutbound["settings"] = map[string]interface{}{
			"servers": []map[string]interface{}{
				{
					"address":  server,
					"port":     xrayPort(outbound["server_port"]),
					"method":   asString(outbound["method"]),
					"password": asString(outbound["password"]),
				},
			},
		}
	case "socks", "http":
		serverConfig := map[string]interface{}{
			"address": server,
			"port":    xrayPort(outbound["server_port"]),
		}
		if username := asString(outbound["username"]); username != "" || asString(outbound["password"]) != "" {
			serverConfig["users"] = []map[string]interface{}{
				{
					"user": username,
					"pass": asString(outbound["password"]),
				},
			}
		}
		xrayOutbound["settings"] = map[string]interface{}{"servers": []map[string]interface{}{serverConfig}}
	default:
		return nil
	}

	if stream := xrayStreamSettings(outbound); len(stream) > 0 {
		xrayOutbound["streamSettings"] = stream
	}
	return xrayOutbound
}
func xrayProtocol(protocol string) string {
	if protocol == "shadowsocks" {
		return "shadowsocks"
	}
	return protocol
}
func xrayStringDefault(value interface{}, fallback string) string {
	if s := strings.TrimSpace(asString(value)); s != "" {
		return s
	}
	return fallback
}
func xrayAlterID(value interface{}) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case uint:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(typed))
		return n
	default:
		return 0
	}
}
func xrayPort(value interface{}) interface{} {
	switch typed := value.(type) {
	case int, int64, uint, float64:
		return typed
	case string:
		if port, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
			return port
		}
		return typed
	default:
		return value
	}
}
func xrayStringList(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []interface{}:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if s := strings.TrimSpace(asString(item)); s != "" {
				result = append(result, s)
			}
		}
		return result
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{strings.TrimSpace(typed)}
	default:
		return nil
	}
}
func appendUniqueXrayString(values []string, next string) []string {
	next = strings.TrimSpace(next)
	if next == "" {
		return values
	}
	for _, existing := range values {
		if existing == next {
			return values
		}
	}
	return append(values, next)
}
