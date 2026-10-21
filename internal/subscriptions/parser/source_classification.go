package parser

import (
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"strings"
)

func sourceDiagnostic(code, field, state string) canonical.ProjectionDiagnostic {
	return canonical.ProjectionDiagnostic{State: state, Code: code, Field: field}
}
func sourceUnknown(object map[string]any, fields string) bool {
	known := map[string]bool{}
	for _, field := range strings.Fields(fields) {
		known[field] = true
	}
	for key := range object {
		if !known[key] {
			return true
		}
	}
	return false
}
func sourceMap(value any) map[string]any { result, _ := value.(map[string]any); return result }

func classifyXrayEndpoint(protocol string, raw map[string]any) []canonical.ProjectionDiagnostic {
	blocked := func(code, field string) []canonical.ProjectionDiagnostic {
		return []canonical.ProjectionDiagnostic{sourceDiagnostic(code, field, canonical.ProjectionManual)}
	}
	if !xraySourceShapes(raw) {
		return blocked("source_field_shape_unmapped", "endpoint")
	}
	if sourceUnknown(raw, "tag protocol settings streamSettings mux sendThrough") {
		return blocked("source_fields_unmapped", "endpoint")
	}
	if value := raw["sendThrough"]; value != nil && value != "" {
		return blocked("source_fields_unmapped", "endpoint")
	}
	if mux := sourceMap(raw["mux"]); len(mux) > 0 && (boolValue(mux["enabled"]) || sourceUnknown(mux, "enabled")) {
		return blocked("source_multiplex_unmapped", "multiplex")
	}
	settings := sourceMap(raw["settings"])
	serverFields, userFields := "address port users", "user pass username password level"
	switch protocol {
	case "vless", "vmess":
		if sourceUnknown(settings, "vnext") {
			return blocked("source_authentication_unmapped", "authentication")
		}
		serverFields = "address port users"
		userFields = "id encryption flow security alterId email level"
	case "trojan", "shadowsocks":
		if sourceUnknown(settings, "servers") {
			return blocked("source_authentication_unmapped", "authentication")
		}
		serverFields = "address port password method email level"
	case "socks", "http":
		if sourceUnknown(settings, "servers") {
			return blocked("source_authentication_unmapped", "authentication")
		}
	default:
		return []canonical.ProjectionDiagnostic{sourceDiagnostic("source_protocol_unsupported", "protocol", canonical.ProjectionUnsupported)}
	}
	servers := settings["servers"]
	if protocol == "vless" || protocol == "vmess" {
		servers = settings["vnext"]
	}
	for _, value := range xrayList(servers) {
		server := sourceMap(value)
		if sourceUnknown(server, serverFields) {
			return blocked("source_authentication_unmapped", "authentication")
		}
		for _, value := range xrayList(server["users"]) {
			user := sourceMap(value)
			if sourceUnknown(user, userFields) {
				return blocked("source_authentication_unmapped", "authentication")
			}
			if protocol == "vless" && stringValue(user["encryption"]) != "" && user["encryption"] != "none" {
				return blocked("source_authentication_unmapped", "authentication")
			}
		}
	}
	stream := sourceMap(raw["streamSettings"])
	if sourceUnknown(stream, "network security tlsSettings realitySettings wsSettings httpSettings grpcSettings httpupgradeSettings tcpSettings rawSettings xhttpSettings splithttpSettings") {
		return blocked("source_fields_unmapped", "transport")
	}
	security := strings.ToLower(stringValue(stream["security"]))
	switch security {
	case "", "none":
	case "tls":
		if sourceUnknown(sourceMap(stream["tlsSettings"]), "serverName allowInsecure alpn fingerprint") {
			return blocked("source_security_unmapped", "tls")
		}
	case "reality":
		reality := sourceMap(stream["realitySettings"])
		if sourceUnknown(reality, "serverName fingerprint publicKey shortId shortIds") || len(xrayStringList(reality["shortIds"])) > 1 {
			return blocked("source_security_unmapped", "tls.reality")
		}
	default:
		return blocked("source_security_unmapped", "tls")
	}
	network := strings.ToLower(stringValue(stream["network"]))
	key, fields := "", ""
	switch network {
	case "", "tcp", "raw":
		key, fields = "tcpSettings", "header"
		tcp := sourceMap(stream[key])
		if network == "raw" {
			tcp = sourceMap(stream["rawSettings"])
		}
		if sourceUnknown(tcp, fields) || sourceUnknown(sourceMap(tcp["header"]), "type") || (stringValue(sourceMap(tcp["header"])["type"]) != "" && sourceMap(tcp["header"])["type"] != "none") {
			return blocked("source_transport_unmapped", "transport")
		}
		return nil
	case "ws":
		key, fields = "wsSettings", "path headers"
	case "http", "h2":
		key, fields = "httpSettings", "path host"
	case "grpc":
		key, fields = "grpcSettings", "serviceName"
	case "httpupgrade":
		key, fields = "httpupgradeSettings", "path host headers"
	case "xhttp", "splithttp":
		return []canonical.ProjectionDiagnostic{sourceDiagnostic("source_transport_unsupported", "transport", canonical.ProjectionUnsupported)}
	default:
		return blocked("source_transport_unmapped", "transport")
	}
	if sourceUnknown(sourceMap(stream[key]), fields) {
		return blocked("source_transport_unmapped", "transport")
	}
	return nil
}

func classifyClashEndpoint(raw map[string]any) []canonical.ProjectionDiagnostic {
	blocked := func(field string) []canonical.ProjectionDiagnostic {
		return []canonical.ProjectionDiagnostic{sourceDiagnostic("source_fields_unmapped", field, canonical.ProjectionManual)}
	}
	known := "name type server port udp tls sni servername skip-cert-verify alpn client-fingerprint reality-opts network ws-opts h2-opts grpc-opts ech-opts"
	switch strings.ToLower(stringValue(raw["type"])) {
	case "vmess":
		known += " uuid alterId cipher"
	case "vless":
		known += " uuid flow"
	case "trojan", "anytls":
		known += " password"
	case "tuic":
		known += " uuid password congestion-controller udp-relay-mode"
	case "ss":
		known += " cipher password udp-over-tcp plugin plugin-opts"
	case "socks", "socks5", "http":
		known += " username password"
	case "hysteria":
		known += " up down auth-str obfs ports hop-interval"
	case "hysteria2":
		known += " up down password obfs obfs-password ports hop-interval"
	default:
		return []canonical.ProjectionDiagnostic{sourceDiagnostic("source_protocol_unsupported", "protocol", canonical.ProjectionUnsupported)}
	}
	if !clashSourceShapes(raw) {
		return blocked("endpoint")
	}
	if sourceUnknown(raw, known) {
		return blocked("endpoint")
	}
	if sourceUnknown(sourceMap(raw["reality-opts"]), "public-key short-id") || sourceUnknown(sourceMap(raw["ech-opts"]), "enable config") {
		return blocked("tls")
	}
	network := strings.ToLower(stringValue(raw["network"]))
	switch network {
	case "", "tcp":
	case "ws":
		ws := sourceMap(raw["ws-opts"])
		if sourceUnknown(ws, "path headers max-early-data early-data-header-name v2ray-http-upgrade") {
			return blocked("transport")
		}
		if boolValue(ws["v2ray-http-upgrade"]) {
			if max, present := ws["max-early-data"]; present && stringValue(max) != "0" {
				return blocked("transport")
			}
			if stringValue(ws["early-data-header-name"]) != "" {
				return blocked("transport")
			}
			for key, value := range sourceMap(ws["headers"]) {
				if values, ok := value.([]any); strings.EqualFold(key, "Host") && ok && len(values) > 1 {
					return blocked("transport")
				}
			}
		}
	case "http", "h2":
		if sourceUnknown(sourceMap(raw["h2-opts"]), "path host") {
			return blocked("transport")
		}
	case "grpc":
		if sourceUnknown(sourceMap(raw["grpc-opts"]), "grpc-service-name") {
			return blocked("transport")
		}
	default:
		return blocked("transport")
	}
	// Explicit UDP restrictions are not discarded during source ingestion.
	// Current runtime projection does not yet encode this native policy.
	if udp, ok := raw["udp"].(bool); ok && !udp {
		return blocked("network")
	}
	return nil
}
