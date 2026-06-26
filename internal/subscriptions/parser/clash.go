package parser

import (
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gopkg.in/yaml.v3"
)

func ParseClashOutbounds(data string) ([]map[string]interface{}, error) {
	return ParseClashOutboundsWithOptions(data, ParseOptions{})
}
func ParseClashOutboundsWithOptions(data string, options ParseOptions) ([]map[string]interface{}, error) {
	var config map[string]interface{}
	if err := yaml.Unmarshal([]byte(data), &config); err != nil {
		return nil, err
	}
	rawProxies, ok := config["proxies"].([]interface{})
	if !ok {
		return nil, common.NewError("invalid clash subscription: missing proxies")
	}
	knownTags := clashKnownTags(config, rawProxies)
	outbounds := make([]map[string]interface{}, 0, len(rawProxies))
	for index, raw := range rawProxies {
		proxy, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		outbound := clashProxyOutbound(proxy, index)
		if outbound == nil {
			continue
		}
		outbounds = append(outbounds, outbound)
	}
	outbounds = append(outbounds, clashProxyGroupOutbounds(config, knownTags, options)...)
	if len(outbounds) == 0 {
		return nil, common.NewError("no result")
	}
	return outbounds, nil
}
func clashKnownTags(config map[string]interface{}, rawProxies []interface{}) map[string]struct{} {
	known := make(map[string]struct{}, len(rawProxies))
	for index, raw := range rawProxies {
		proxy, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		known[clashProxyName(proxy, index)] = struct{}{}
	}
	for index, raw := range xrayList(config["proxy-groups"]) {
		group, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name := strings.TrimSpace(stringValue(group["name"]))
		if name == "" {
			name = fmt.Sprintf("clash-group-%d", index+1)
		}
		known[name] = struct{}{}
	}
	return known
}
func clashProxyOutbound(proxy map[string]interface{}, index int) map[string]interface{} {
	proxyType := strings.ToLower(strings.TrimSpace(stringValue(proxy["type"])))
	outboundType := clashOutboundType(proxyType)
	if outboundType == "" {
		return nil
	}
	outbound := map[string]interface{}{
		"type":        outboundType,
		"tag":         clashProxyName(proxy, index),
		"server":      stringValue(proxy["server"]),
		"server_port": proxy["port"],
	}
	switch outboundType {
	case "vmess", "vless", "tuic":
		copyString(proxy, outbound, "uuid", "uuid")
		if outboundType == "vmess" {
			copyAny(proxy, outbound, "alterId", "alter_id")
			if _, ok := outbound["alter_id"]; !ok {
				outbound["alter_id"] = 0
			}
			outbound["security"] = "auto"
		}
		if outboundType == "vless" {
			copyString(proxy, outbound, "flow", "flow")
		}
		if outboundType == "tuic" {
			copyString(proxy, outbound, "password", "password")
			copyString(proxy, outbound, "congestion-controller", "congestion_control")
			copyString(proxy, outbound, "udp-relay-mode", "udp_relay_mode")
		}
	case "trojan", "anytls":
		copyString(proxy, outbound, "password", "password")
	case "shadowsocks":
		copyString(proxy, outbound, "cipher", "method")
		copyString(proxy, outbound, "password", "password")
		if boolValue(proxy["udp-over-tcp"]) {
			outbound["udp_over_tcp"] = true
		}
	case "socks", "http":
		copyString(proxy, outbound, "username", "username")
		copyString(proxy, outbound, "password", "password")
	case "hysteria":
		copyAny(proxy, outbound, "up", "up_mbps")
		copyAny(proxy, outbound, "down", "down_mbps")
		copyString(proxy, outbound, "auth-str", "auth_str")
		copyString(proxy, outbound, "obfs", "obfs")
	case "hysteria2":
		copyAny(proxy, outbound, "up", "up_mbps")
		copyAny(proxy, outbound, "down", "down_mbps")
		copyString(proxy, outbound, "password", "password")
		if obfs := stringValue(proxy["obfs"]); obfs != "" {
			outbound["obfs"] = map[string]interface{}{
				"type":     obfs,
				"password": stringValue(proxy["obfs-password"]),
			}
		}
	}
	if tls := clashTLS(proxy); len(tls) > 0 {
		outbound["tls"] = tls
	}
	if transport := clashTransport(proxy); len(transport) > 0 {
		outbound["transport"] = transport
	}
	return outbound
}
func clashOutboundType(proxyType string) string {
	switch proxyType {
	case "ss":
		return "shadowsocks"
	case "socks5":
		return "socks"
	case "http", "socks", "vmess", "vless", "trojan", "tuic", "hysteria", "hysteria2", "anytls":
		return proxyType
	default:
		return ""
	}
}
func clashTLS(proxy map[string]interface{}) map[string]interface{} {
	tls := map[string]interface{}{}
	enabled := boolValue(proxy["tls"])
	if enabled {
		tls["enabled"] = true
	}
	if serverName := firstString(proxy["sni"], proxy["servername"]); serverName != "" {
		enabled = true
		tls["server_name"] = serverName
	}
	if boolValue(proxy["skip-cert-verify"]) {
		enabled = true
		tls["insecure"] = true
	}
	if alpn, ok := proxy["alpn"].([]interface{}); ok && len(alpn) > 0 {
		enabled = true
		tls["alpn"] = alpn
	}
	if reality, ok := proxy["reality-opts"].(map[string]interface{}); ok && len(reality) > 0 {
		enabled = true
		tls["reality"] = map[string]interface{}{
			"enabled":    true,
			"public_key": stringValue(reality["public-key"]),
			"short_id":   stringValue(reality["short-id"]),
		}
	}
	if fingerprint := stringValue(proxy["client-fingerprint"]); fingerprint != "" && enabled {
		tls["utls"] = map[string]interface{}{
			"enabled":     true,
			"fingerprint": fingerprint,
		}
	}
	if enabled {
		tls["enabled"] = true
	}
	return tls
}
func clashTransport(proxy map[string]interface{}) map[string]interface{} {
	network := strings.ToLower(strings.TrimSpace(stringValue(proxy["network"])))
	switch network {
	case "ws":
		opts, _ := proxy["ws-opts"].(map[string]interface{})
		transport := map[string]interface{}{"type": "ws"}
		copyString(opts, transport, "path", "path")
		if headers, ok := opts["headers"].(map[string]interface{}); ok && len(headers) > 0 {
			transport["headers"] = headers
		}
		copyString(opts, transport, "early-data-header-name", "early_data_header_name")
		return transport
	case "h2", "http":
		opts, _ := proxy["h2-opts"].(map[string]interface{})
		transport := map[string]interface{}{"type": "http"}
		if path := firstString(opts["path"]); path != "" {
			transport["path"] = path
		}
		if host := firstString(opts["host"]); host != "" {
			transport["host"] = []interface{}{host}
		}
		return transport
	case "grpc":
		opts, _ := proxy["grpc-opts"].(map[string]interface{})
		transport := map[string]interface{}{"type": "grpc"}
		copyString(opts, transport, "grpc-service-name", "service_name")
		return transport
	default:
		return nil
	}
}
func clashProxyName(proxy map[string]interface{}, index int) string {
	if name := stringValue(proxy["name"]); name != "" {
		return name
	}
	return fmt.Sprintf("clash-%d", index+1)
}
func copyString(src map[string]interface{}, dst map[string]interface{}, srcKey string, dstKey string) {
	if src == nil {
		return
	}
	if value := stringValue(src[srcKey]); value != "" {
		dst[dstKey] = value
	}
}
func copyAny(src map[string]interface{}, dst map[string]interface{}, srcKey string, dstKey string) {
	if src == nil {
		return
	}
	if value, ok := src[srcKey]; ok {
		dst[dstKey] = value
	}
}
func firstString(values ...interface{}) string {
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case []interface{}:
			for _, item := range typed {
				if value := strings.TrimSpace(stringValue(item)); value != "" {
					return value
				}
			}
		}
	}
	return ""
}
func stringValue(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case uint:
		return fmt.Sprintf("%d", typed)
	case float64:
		return fmt.Sprintf("%.0f", typed)
	default:
		return ""
	}
}
func boolValue(value interface{}) bool {
	typed, _ := value.(bool)
	return typed
}
