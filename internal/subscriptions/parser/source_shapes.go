package parser

import "strings"

// Check only the source structure that the existing parser would otherwise
// omit. Rejecting the whole input keeps a last accepted snapshot atomic.
func xraySourceHasEndpoints(raw map[string]any) bool {
	field := ""
	switch strings.ToLower(stringValue(raw["protocol"])) {
	case "":
		return false
	case "vless", "vmess":
		field = "vnext"
	case "trojan", "shadowsocks", "socks", "http":
		field = "servers"
	default:
		return true
	}
	servers, ok := sourceMap(raw["settings"])[field].([]any)
	if !ok || len(servers) == 0 {
		return false
	}
	for _, value := range servers {
		server, ok := value.(map[string]any)
		if !ok || strings.TrimSpace(stringValue(server["address"])) == "" || server["port"] == nil {
			return false
		}
	}
	return true
}

// Inspect native carriers before a parser can coerce an invalid flag or
// credential into an absent/default value. Diagnostics use fixed paths.
func sourceShapes(object map[string]any, textFields, boolFields, listFields string) bool {
	for _, key := range strings.Fields(textFields) {
		if value, present := object[key]; present {
			if _, ok := value.(string); !ok {
				return false
			}
		}
	}
	for _, key := range strings.Fields(boolFields) {
		if value, present := object[key]; present {
			if _, ok := value.(bool); !ok {
				return false
			}
		}
	}
	for _, key := range strings.Fields(listFields) {
		if value, present := object[key]; present && !sourceStringList(value) {
			return false
		}
	}
	return true
}

func sourceStringList(value any) bool {
	switch values := value.(type) {
	case string, []string:
		return true
	case []any:
		for _, value := range values {
			if _, ok := value.(string); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func sourceObjects(object map[string]any, fields string) bool {
	for _, key := range strings.Fields(fields) {
		if value, present := object[key]; present && value != nil {
			if _, ok := value.(map[string]any); !ok {
				return false
			}
		}
	}
	return true
}

func sourceHeaders(value any) bool {
	if value == nil {
		return true
	}
	headers, ok := value.(map[string]any)
	if !ok {
		return false
	}
	for _, value := range headers {
		if !sourceStringList(value) {
			return false
		}
	}
	return true
}

func xraySourceShapes(raw map[string]any) bool {
	if !sourceShapes(raw, "protocol tag sendThrough", "", "") || !sourceObjects(raw, "settings streamSettings mux") {
		return false
	}
	if !sourceShapes(sourceMap(raw["mux"]), "", "enabled", "") {
		return false
	}
	settings := sourceMap(raw["settings"])
	for _, key := range []string{"servers", "vnext"} {
		value, present := settings[key]
		if !present {
			continue
		}
		servers, ok := value.([]any)
		if !ok {
			return false
		}
		for _, value := range servers {
			server, ok := value.(map[string]any)
			if !ok || !sourceShapes(server, "address password method", "", "") {
				return false
			}
			if users, present := server["users"]; present {
				users, ok := users.([]any)
				if !ok {
					return false
				}
				for _, value := range users {
					user, ok := value.(map[string]any)
					if !ok || !sourceShapes(user, "id encryption flow security user pass username password", "", "") {
						return false
					}
				}
			}
		}
	}
	stream := sourceMap(raw["streamSettings"])
	if !sourceShapes(stream, "network security", "", "") || !sourceObjects(stream, "tlsSettings realitySettings wsSettings httpSettings grpcSettings httpupgradeSettings tcpSettings rawSettings xhttpSettings splithttpSettings") {
		return false
	}
	if !sourceShapes(sourceMap(stream["tlsSettings"]), "serverName fingerprint", "allowInsecure", "alpn") ||
		!sourceShapes(sourceMap(stream["realitySettings"]), "serverName fingerprint publicKey shortId", "", "shortIds") {
		return false
	}
	for _, key := range []string{"wsSettings", "httpSettings", "grpcSettings", "httpupgradeSettings"} {
		transport := sourceMap(stream[key])
		if !sourceShapes(transport, "path serviceName", "", "host") || !sourceHeaders(transport["headers"]) {
			return false
		}
	}
	return true
}

func clashSourceShapes(raw map[string]any) bool {
	if !sourceShapes(raw, "name type server sni servername client-fingerprint network uuid password username cipher flow congestion-controller udp-relay-mode auth-str obfs obfs-password plugin", "udp tls skip-cert-verify udp-over-tcp", "alpn") ||
		!sourceObjects(raw, "reality-opts ech-opts ws-opts h2-opts grpc-opts plugin-opts") {
		return false
	}
	if !sourceShapes(sourceMap(raw["reality-opts"]), "public-key short-id", "", "") ||
		!sourceShapes(sourceMap(raw["ech-opts"]), "", "enable", "config") {
		return false
	}
	ws := sourceMap(raw["ws-opts"])
	if !sourceShapes(ws, "path early-data-header-name", "v2ray-http-upgrade", "") || !sourceHeaders(ws["headers"]) {
		return false
	}
	return sourceShapes(sourceMap(raw["h2-opts"]), "path", "", "host") &&
		sourceShapes(sourceMap(raw["grpc-opts"]), "grpc-service-name", "", "")
}
