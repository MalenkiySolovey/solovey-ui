package formats

import (
	"strings"
)

func xrayStreamSettings(outbound map[string]interface{}) map[string]interface{} {
	stream := map[string]interface{}{}
	if tls := xrayStreamTLS(outbound); len(tls) > 0 {
		for key, value := range tls {
			stream[key] = value
		}
	}
	if transport := xrayStreamTransport(outbound); len(transport) > 0 {
		for key, value := range transport {
			stream[key] = value
		}
	}
	return stream
}
func xrayStreamTLS(outbound map[string]interface{}) map[string]interface{} {
	tls, _ := outbound["tls"].(map[string]interface{})
	if len(tls) == 0 || !asBool(tls["enabled"]) {
		return nil
	}
	if reality, ok := tls["reality"].(map[string]interface{}); ok && (asBool(reality["enabled"]) || asString(reality["public_key"]) != "") {
		settings := map[string]interface{}{}
		if serverName := asString(tls["server_name"]); serverName != "" {
			settings["serverName"] = serverName
		}
		if publicKey := asString(reality["public_key"]); publicKey != "" {
			settings["publicKey"] = publicKey
		}
		if shortID := asString(reality["short_id"]); shortID != "" {
			settings["shortId"] = shortID
		}
		if utls, ok := tls["utls"].(map[string]interface{}); ok {
			if fingerprint := asString(utls["fingerprint"]); fingerprint != "" {
				settings["fingerprint"] = fingerprint
			}
		}
		return map[string]interface{}{
			"security":        "reality",
			"realitySettings": settings,
		}
	}

	settings := map[string]interface{}{}
	if serverName := asString(tls["server_name"]); serverName != "" {
		settings["serverName"] = serverName
	}
	if insecure := asBool(tls["insecure"]); insecure {
		settings["allowInsecure"] = true
	}
	if alpn := xrayStringList(tls["alpn"]); len(alpn) > 0 {
		settings["alpn"] = alpn
	}
	if utls, ok := tls["utls"].(map[string]interface{}); ok {
		if fingerprint := asString(utls["fingerprint"]); fingerprint != "" {
			settings["fingerprint"] = fingerprint
		}
	}
	return map[string]interface{}{
		"security":    "tls",
		"tlsSettings": settings,
	}
}
func xrayStreamTransport(outbound map[string]interface{}) map[string]interface{} {
	transport, _ := outbound["transport"].(map[string]interface{})
	transportType := strings.TrimSpace(asString(transport["type"]))
	switch transportType {
	case "":
		return nil
	case "ws":
		settings := map[string]interface{}{}
		if path := asString(transport["path"]); path != "" {
			settings["path"] = path
		}
		if headers, ok := transport["headers"].(map[string]interface{}); ok && len(headers) > 0 {
			settings["headers"] = headers
		}
		return map[string]interface{}{
			"network":    "ws",
			"wsSettings": settings,
		}
	case "http", "h2":
		settings := map[string]interface{}{}
		if path := asString(transport["path"]); path != "" {
			settings["path"] = []string{path}
		}
		if host := xrayStringList(transport["host"]); len(host) > 0 {
			settings["host"] = host
		}
		return map[string]interface{}{
			"network":      "http",
			"httpSettings": settings,
		}
	case "grpc":
		settings := map[string]interface{}{}
		if serviceName := asString(transport["service_name"]); serviceName != "" {
			settings["serviceName"] = serviceName
		}
		return map[string]interface{}{
			"network":      "grpc",
			"grpcSettings": settings,
		}
	case "httpupgrade":
		settings := map[string]interface{}{}
		if path := asString(transport["path"]); path != "" {
			settings["path"] = path
		}
		if host := asString(transport["host"]); host != "" {
			settings["host"] = host
		}
		if headers, ok := transport["headers"].(map[string]interface{}); ok && len(headers) > 0 {
			settings["headers"] = headers
		}
		return map[string]interface{}{
			"network":             "httpupgrade",
			"httpupgradeSettings": settings,
		}
	default:
		return nil
	}
}
