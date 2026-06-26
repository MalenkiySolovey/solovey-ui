package parser

import (
	"fmt"
	"strings"
)

func xrayProtocolOutbounds(protocol string, outbound map[string]interface{}, index int) []map[string]interface{} {
	settings, _ := outbound["settings"].(map[string]interface{})
	stream, _ := outbound["streamSettings"].(map[string]interface{})

	var result []map[string]interface{}
	switch protocol {
	case "vless", "vmess":
		for _, rawServer := range xrayList(settings["vnext"]) {
			server, ok := rawServer.(map[string]interface{})
			if !ok {
				continue
			}
			for _, rawUser := range xrayUsers(server["users"]) {
				user, _ := rawUser.(map[string]interface{})
				node := xrayServerOutbound(protocol, server)
				copyString(user, node, "id", "uuid")
				copyString(user, node, "flow", "flow")
				if protocol == "vmess" {
					copyAny(user, node, "alterId", "alter_id")
					if _, ok := node["alter_id"]; !ok {
						node["alter_id"] = 0
					}
					if security := stringValue(user["security"]); security != "" {
						node["security"] = security
					} else {
						node["security"] = "auto"
					}
				}
				result = append(result, node)
			}
		}
	case "trojan", "shadowsocks":
		for _, rawServer := range xrayList(settings["servers"]) {
			server, ok := rawServer.(map[string]interface{})
			if !ok {
				continue
			}
			node := xrayServerOutbound(protocol, server)
			copyString(server, node, "password", "password")
			if protocol == "shadowsocks" {
				copyString(server, node, "method", "method")
			}
			result = append(result, node)
		}
	case "socks", "http":
		for _, rawServer := range xrayList(settings["servers"]) {
			server, ok := rawServer.(map[string]interface{})
			if !ok {
				continue
			}
			for _, rawUser := range xrayUsers(server["users"]) {
				user, _ := rawUser.(map[string]interface{})
				node := xrayServerOutbound(protocol, server)
				copyString(user, node, "user", "username")
				copyString(user, node, "pass", "password")
				copyString(user, node, "username", "username")
				copyString(user, node, "password", "password")
				result = append(result, node)
			}
		}
	default:
		return nil
	}

	filtered := result[:0]
	for _, node := range result {
		if node["server"] == "" || node["server_port"] == nil {
			continue
		}
		if !xrayApplyStreamSettings(node, stream) {
			continue
		}
		filtered = append(filtered, node)
	}
	return filtered
}
func xrayServerOutbound(protocol string, server map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type":        protocol,
		"server":      stringValue(server["address"]),
		"server_port": server["port"],
	}
}
func xrayUsers(value interface{}) []interface{} {
	users := xrayList(value)
	if len(users) > 0 {
		return users
	}
	return []interface{}{map[string]interface{}{}}
}
func xrayApplyStreamSettings(outbound map[string]interface{}, stream map[string]interface{}) bool {
	network := strings.ToLower(strings.TrimSpace(stringValue(stream["network"])))
	switch network {
	case "", "tcp", "raw":
	case "ws", "http", "h2", "grpc", "httpupgrade", "splithttp", "xhttp":
		if transport := xrayTransport(stream, network); len(transport) > 0 {
			outbound["transport"] = transport
		}
		if network == "splithttp" || network == "xhttp" {
			markXrayTransportAdaptation(outbound, network, "httpupgrade", "Xray XHTTP/SplitHTTP has no native sing-box transport; mapped to httpupgrade for preservation")
		}
	default:
		return false
	}
	if tls := xrayTLS(stream); len(tls) > 0 {
		outbound["tls"] = tls
	}
	return true
}
func xrayTLS(stream map[string]interface{}) map[string]interface{} {
	security := strings.ToLower(strings.TrimSpace(stringValue(stream["security"])))
	switch security {
	case "tls":
		settings, _ := stream["tlsSettings"].(map[string]interface{})
		tls := map[string]interface{}{"enabled": true}
		copyString(settings, tls, "serverName", "server_name")
		if boolValue(settings["allowInsecure"]) {
			tls["insecure"] = true
		}
		if alpn := xrayStringList(settings["alpn"]); len(alpn) > 0 {
			tls["alpn"] = alpn
		}
		if fingerprint := stringValue(settings["fingerprint"]); fingerprint != "" {
			tls["utls"] = map[string]interface{}{
				"enabled":     true,
				"fingerprint": fingerprint,
			}
		}
		return tls
	case "reality":
		settings, _ := stream["realitySettings"].(map[string]interface{})
		tls := map[string]interface{}{
			"enabled": true,
			"reality": map[string]interface{}{
				"enabled":    true,
				"public_key": stringValue(settings["publicKey"]),
				"short_id":   firstString(settings["shortId"], settings["shortIds"]),
			},
		}
		copyString(settings, tls, "serverName", "server_name")
		if fingerprint := stringValue(settings["fingerprint"]); fingerprint != "" {
			tls["utls"] = map[string]interface{}{
				"enabled":     true,
				"fingerprint": fingerprint,
			}
		}
		return tls
	default:
		return nil
	}
}
func xrayTransport(stream map[string]interface{}, network string) map[string]interface{} {
	switch network {
	case "ws":
		settings, _ := stream["wsSettings"].(map[string]interface{})
		transport := map[string]interface{}{"type": "ws"}
		copyString(settings, transport, "path", "path")
		if headers, ok := settings["headers"].(map[string]interface{}); ok && len(headers) > 0 {
			transport["headers"] = headers
		}
		return transport
	case "http", "h2":
		settings, _ := stream["httpSettings"].(map[string]interface{})
		transport := map[string]interface{}{"type": "http"}
		if path := firstString(settings["path"]); path != "" {
			transport["path"] = path
		}
		if host := xrayStringList(settings["host"]); len(host) > 0 {
			transport["host"] = host
		}
		return transport
	case "grpc":
		settings, _ := stream["grpcSettings"].(map[string]interface{})
		transport := map[string]interface{}{"type": "grpc"}
		copyString(settings, transport, "serviceName", "service_name")
		return transport
	case "httpupgrade":
		settings, _ := stream["httpupgradeSettings"].(map[string]interface{})
		transport := map[string]interface{}{"type": "httpupgrade"}
		copyString(settings, transport, "path", "path")
		copyString(settings, transport, "host", "host")
		if headers, ok := settings["headers"].(map[string]interface{}); ok && len(headers) > 0 {
			transport["headers"] = headers
		}
		return transport
	case "splithttp", "xhttp":
		settings, _ := stream["xhttpSettings"].(map[string]interface{})
		if len(settings) == 0 {
			settings, _ = stream["splithttpSettings"].(map[string]interface{})
		}
		transport := map[string]interface{}{"type": "httpupgrade"}
		copyString(settings, transport, "path", "path")
		copyString(settings, transport, "host", "host")
		if headers, ok := settings["headers"].(map[string]interface{}); ok && len(headers) > 0 {
			transport["headers"] = headers
		}
		return transport
	default:
		return nil
	}
}
func xrayOutboundBaseTag(outbound map[string]interface{}, index int) string {
	if tag := strings.TrimSpace(stringValue(outbound["tag"])); tag != "" {
		return tag
	}
	return fmt.Sprintf("xray-%d", index+1)
}
func xrayVariantTag(base string, index int, total int) string {
	if total <= 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, index+1)
}
