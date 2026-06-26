package uri

import (
	"encoding/json"
	"fmt"
	"strings"
)

func anytlsLink(
	userConfig map[string]interface{},
	addrs []map[string]interface{}) []string {
	password, _ := userConfig["password"].(string)
	baseUri := fmt.Sprintf("%s%s@", "anytls://", password)
	var links []string
	for _, addr := range addrs {
		var params []LinkParam
		if tls, ok := addr["tls"].(map[string]interface{}); ok {
			getTlsParams(&params, tls, "insecure")
		}
		port, _ := addr["server_port"].(float64)
		uri := fmt.Sprintf("%s%s:%.0f", baseUri, mapString(addr, "server"), port)
		links = append(links, addParams(uri, params, mapString(addr, "remark")))
	}
	return links
}
func tuicLink(
	userConfig map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {
	password, _ := userConfig["password"].(string)
	uuid, _ := userConfig["uuid"].(string)
	baseUri := fmt.Sprintf("%s%s:%s@", "tuic://", uuid, password)
	udpRelayMode := tuicUDPRelayMode(inbound)
	var links []string
	for _, addr := range addrs {
		var params []LinkParam
		if tls, ok := addr["tls"].(map[string]interface{}); ok {
			getTlsParams(&params, tls, "insecure")
		}
		if congestionControl, ok := inbound["congestion_control"].(string); ok {
			params = append(params, LinkParam{"congestion_control", congestionControl})
		}
		if udpRelayMode != "" {
			params = append(params, LinkParam{"udp_relay_mode", udpRelayMode})
		}
		port, _ := addr["server_port"].(float64)
		uri := fmt.Sprintf("%s%s:%.0f", baseUri, mapString(addr, "server"), port)
		links = append(links, addParams(uri, params, mapString(addr, "remark")))
	}
	return links
}
func tuicUDPRelayMode(inbound map[string]interface{}) string {
	if outJson, ok := inbound["out_json"].(json.RawMessage); ok {
		var out map[string]interface{}
		if err := json.Unmarshal(outJson, &out); err == nil {
			if mode := normalizeTUICUDPRelayMode(out["udp_relay_mode"]); mode != "" {
				return mode
			}
		}
	}
	if outJson, ok := inbound["out_json"].(map[string]interface{}); ok {
		if mode := normalizeTUICUDPRelayMode(outJson["udp_relay_mode"]); mode != "" {
			return mode
		}
	}
	if mode := normalizeTUICUDPRelayMode(inbound["udp_relay_mode"]); mode != "" {
		return mode
	}
	return defaultTUICUDPRelayMode
}
func normalizeTUICUDPRelayMode(value interface{}) string {
	mode, _ := value.(string)
	switch strings.TrimSpace(mode) {
	case "native", "quic":
		return strings.TrimSpace(mode)
	default:
		return ""
	}
}
func vlessLink(
	userConfig map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {
	uuid, _ := userConfig["uuid"].(string)
	baseParams := getTransportParams(inbound["transport"])
	var links []string
	// `xtls-rprx-vision` is strictly TCP. Emitting it on a vless link
	// whose transport is grpc / ws / http / httpupgrade makes Xray-core
	// reject the connection on the client side (issue #1127). Decide
	// once per inbound so we never produce a self-broken link.
	transportType := "tcp"
	if tr, ok := inbound["transport"].(map[string]interface{}); ok {
		if tt, _ := tr["type"].(string); tt != "" {
			transportType = tt
		}
	}
	for _, addr := range addrs {
		params := make([]LinkParam, len(baseParams))
		copy(params, baseParams)
		if tls, ok := addr["tls"].(map[string]interface{}); ok && asBool(tls["enabled"]) {
			getTlsParams(&params, tls, "allowInsecure")
			if flow, ok := userConfig["flow"].(string); ok && flow != "" && transportType == "tcp" {
				params = append(params, LinkParam{"flow", flow})
			}
		}
		port, _ := addr["server_port"].(float64)
		uri := fmt.Sprintf("vless://%s@%s:%.0f", uuid, mapString(addr, "server"), port)
		uri = addParams(uri, params, mapString(addr, "remark"))
		links = append(links, uri)
	}
	return links
}
func trojanLink(
	userConfig map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {
	password, _ := userConfig["password"].(string)
	baseParams := getTransportParams(inbound["transport"])
	var links []string
	for _, addr := range addrs {
		params := make([]LinkParam, len(baseParams))
		copy(params, baseParams)
		if tls, ok := addr["tls"].(map[string]interface{}); ok && asBool(tls["enabled"]) {
			getTlsParams(&params, tls, "allowInsecure")
		}
		port, _ := addr["server_port"].(float64)
		uri := fmt.Sprintf("trojan://%s@%s:%.0f", password, mapString(addr, "server"), port)
		uri = addParams(uri, params, mapString(addr, "remark"))
		links = append(links, uri)
	}
	return links
}
func vmessLink(
	userConfig map[string]interface{},
	inbound map[string]interface{},
	addrs []map[string]interface{}) []string {
	uuid, _ := userConfig["uuid"].(string)
	transportParams := getTransportParams(inbound["transport"])
	var links []string
	baseParams := map[string]interface{}{
		"v":   "2",
		"id":  uuid,
		"aid": 0,
	}
	var net, typ, host, path string
	for _, p := range transportParams {
		switch p.Key {
		case "type":
			net = p.Value
		case "host":
			host = p.Value
		case "path":
			path = p.Value
		}
	}
	if net == "http" || net == "tcp" {
		baseParams["net"] = "tcp"
		if net == "http" {
			typ = "http"
		}
	} else {
		baseParams["net"] = net
	}
	for _, addr := range addrs {
		obj := make(map[string]interface{})
		for k, v := range baseParams {
			obj[k] = v
		}
		obj["add"], _ = addr["server"].(string)
		port, _ := addr["server_port"].(float64)
		obj["port"] = fmt.Sprintf("%.0f", port)
		obj["ps"], _ = addr["remark"].(string)
		if typ != "" {
			obj["type"] = typ
		}
		if host != "" {
			obj["host"] = host
		}
		if path != "" {
			obj["path"] = path
		}
		populateVmessTlsParams(obj, addr["tls"])
		jsonStr, _ := json.Marshal(obj)
		uri := fmt.Sprintf("vmess://%s", toBase64(jsonStr))
		links = append(links, uri)
	}
	return links
}
func populateVmessTlsParams(obj map[string]interface{}, tlsConfig interface{}) {
	if tlsMap, ok := tlsConfig.(map[string]interface{}); ok && asBool(tlsMap["enabled"]) {
		obj["tls"] = "tls"
		var tlsParams []LinkParam
		getTlsParams(&tlsParams, tlsMap, "allowInsecure")
		for _, p := range tlsParams {
			switch p.Key {
			case "security":
				// ignore, as "tls" is already set
			case "allowInsecure":
				obj["allowInsecure"] = 1
			case "sni":
				obj["sni"] = p.Value
			case "fp":
				obj["fp"] = p.Value
			case "alpn":
				obj["alpn"] = p.Value
			}
		}
	} else {
		obj["tls"] = "none"
	}
}
