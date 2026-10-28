package canonical

import (
	"encoding/json"
	"errors"
	"strings"

	entityprotocol "github.com/MalenkiySolovey/solovey-ui/internal/entities/protocol"
)

var ErrUnsupportedProjection = errors.New("UNSUPPORTED: endpoint fields cannot be preserved in the requested representation")

// ProjectOutbounds is the shared IR boundary for current serializers. Target
// adapters retain their current config shells and declared group policies.
func ProjectOutbounds(target string, outbounds []map[string]any) ([]map[string]any, error) {
	result := make([]map[string]any, 0, len(outbounds))
	for index, raw := range outbounds {
		if raw == nil {
			return nil, ErrUnsupportedProjection
		}
		endpoint := ObserveOutbound(FormatSingBox, raw, index)
		projected, err := CheckedEndpointOutbound(endpoint)
		if err != nil {
			return nil, err
		}
		if err := ValidateEndpointProjection(endpoint, target); err != nil {
			return nil, err
		}
		// Native target observations are representation metadata, not runtime
		// options. Preserve only the current serializer's own native metadata.
		for key, value := range raw {
			if key == MetadataKey || (target == FormatClash && strings.HasPrefix(key, "mihomo_")) || (target == FormatXray && strings.HasPrefix(key, "xray_")) {
				projected[key] = cloneValue(value)
			}
		}
		// Public labels belong to the target's existing naming owner. An IR
		// observation's display fallback must not become a new explicit tag.
		if strings.TrimSpace(stringValue(raw["tag"])) == "" {
			delete(projected, "tag")
		}
		// Format adapters consume JSON listables. The typed IR can contain
		// []string as well as decoded []any; both must preserve the same facts.
		encoded, err := json.Marshal(projected)
		if err != nil || json.Unmarshal(encoded, &projected) != nil {
			return nil, ErrUnsupportedProjection
		}
		result = append(result, projected)
	}
	return result, nil
}

func ValidateEndpointProjection(endpoint EndpointIR, target string) error {
	raw := EndpointOutbound(endpoint)
	protocol := endpoint.Protocol
	switch protocol {
	case "selector", "urltest", "failover", "direct", "block":
		return nil
	}
	if target == FormatSingBox {
		return nil
	} // full options survive; existing shape/owner validation follows
	if protocol == "mieru" && target == FormatURI {
		if unknownKeys(raw, "type tag server server_port server_ports username password transport multiplexing handshake_mode traffic_pattern mtu") {
			return ErrUnsupportedProjection
		}
		return nil
	}
	allowed := "type tag name remark remarks ps id sortOrder sort_order server server_port tls transport network"
	switch target {
	case FormatClash:
		allowed += " multiplex"
		switch protocol {
		case "vmess":
			allowed += " uuid alter_id security"
		case "vless":
			allowed += " uuid flow"
		case "tuic":
			allowed += " uuid password congestion_control udp_relay_mode"
		case "trojan", "anytls":
			allowed += " password"
		case "socks", "http":
			allowed += " username password version"
		case "shadowsocks":
			allowed += " method password udp_over_tcp plugin plugin_opts"
		case "hysteria":
			allowed += " auth_str obfs up_mbps down_mbps server_ports hop_interval"
		case "hysteria2":
			allowed += " password obfs up_mbps down_mbps server_ports hop_interval"
		default:
			return ErrUnsupportedProjection
		}
	case FormatXray:
		switch protocol {
		case "vmess":
			allowed += " uuid flow alter_id security"
		case "vless":
			allowed += " uuid flow"
		case "trojan":
			allowed += " password flow"
		case "shadowsocks":
			allowed += " method password"
		case "socks", "http":
			allowed += " username password version"
		default:
			return ErrUnsupportedProjection
		}
	case FormatURI:
		// URI generation retains its protocol-specific encoder. This shared
		// check qualifies security/transport facts before those encoders run.
		switch protocol {
		case "vless":
			allowed += " uuid flow"
		case "vmess":
			allowed += " uuid alter_id security"
		case "trojan", "anytls":
			allowed += " password"
		default:
			return ErrUnsupportedProjection
		}
	default:
		return ErrUnsupportedProjection
	}
	if unknownKeys(raw, allowed) {
		return ErrUnsupportedProjection
	}
	if version := stringValue(raw["version"]); protocol == "socks" && version != "" && version != "5" {
		return ErrUnsupportedProjection
	}
	if network, present := raw["network"]; present && !isEmpty(network) && stringValue(network) != "tcp" {
		values := stringList(network)
		if len(values) != 2 || !((values[0] == "tcp" && values[1] == "udp") || (values[0] == "udp" && values[1] == "tcp")) {
			return ErrUnsupportedProjection
		}
	}
	if tls, ok := raw["tls"].(map[string]any); ok {
		if target == FormatClash && protocol == "anytls" && endpoint.TLS.Reality {
			return ErrUnsupportedProjection
		}
		keys := "enabled server_name insecure alpn utls reality"
		if target == FormatClash {
			keys += " ech"
		}
		if target == FormatURI && protocol != "vmess" {
			keys += " disable_sni certificate_public_key_sha256"
			if moreThanOne(tls["certificate_public_key_sha256"]) {
				return ErrUnsupportedProjection
			}
		}
		if unknownKeys(tls, keys) {
			return ErrUnsupportedProjection
		}
		if utls, ok := tls["utls"].(map[string]any); ok && unknownKeys(utls, "enabled fingerprint") {
			return ErrUnsupportedProjection
		}
		if reality, ok := tls["reality"].(map[string]any); ok && unknownKeys(reality, "enabled public_key short_id") {
			return ErrUnsupportedProjection
		}
		if ech, ok := tls["ech"].(map[string]any); ok && unknownKeys(ech, "enabled config") {
			return ErrUnsupportedProjection
		}
		if target == FormatXray && endpoint.TLS.Reality && (!isEmpty(tls["alpn"]) || boolValue(tls["insecure"])) {
			return ErrUnsupportedProjection
		}
		if target == FormatURI && protocol == "vmess" && endpoint.TLS.Reality {
			return ErrUnsupportedProjection
		}
	}
	if transport, ok := raw["transport"].(map[string]any); ok {
		keys := "type"
		switch stringValue(transport["type"]) {
		case "", "tcp":
		case "ws":
			keys += " path headers"
			if target != FormatXray {
				keys += " max_early_data early_data_header_name"
			}
		case "httpupgrade":
			keys += " path host headers"
		case "http", "h2":
			keys += " path host"
		case "grpc":
			keys += " service_name"
		case "xhttp":
			if padding, present := transport["x_padding_bytes"]; present && padding != entityprotocol.XHTTPDefaultPadding {
				return ErrUnsupportedProjection
			}
			if target == FormatClash || (target == FormatURI && protocol != "vless" && protocol != "trojan") {
				return ErrUnsupportedProjection
			}
			keys += " path host mode x_padding_bytes"
			if target == FormatXray {
				keys += " headers"
			}
		default:
			return ErrUnsupportedProjection
		}
		if unknownKeys(transport, keys) {
			return ErrUnsupportedProjection
		}
		if target == FormatURI {
			if headers, ok := transport["headers"].(map[string]any); ok && unknownKeys(headers, "Host") {
				return ErrUnsupportedProjection
			}
			if stringValue(transport["type"]) == "httpupgrade" && !isEmpty(transport["headers"]) {
				return ErrUnsupportedProjection
			}
		}
		if target == FormatClash {
			if moreThanOne(transport["host"]) || moreThanOne(transport["path"]) {
				return ErrUnsupportedProjection
			}
			if headers, ok := transport["headers"].(map[string]any); ok {
				for _, value := range headers {
					if moreThanOne(value) {
						return ErrUnsupportedProjection
					}
				}
			}
		}
	}
	if mux, ok := raw["multiplex"].(map[string]any); ok {
		if target != FormatClash {
			if len(mux) > 1 || boolValue(mux["enabled"]) {
				return ErrUnsupportedProjection
			}
		} else {
			if unknownKeys(mux, "enabled protocol max_connections min_streams max_streams padding brutal") {
				return ErrUnsupportedProjection
			}
			if brutal, ok := mux["brutal"].(map[string]any); ok && unknownKeys(brutal, "enabled up_mbps down_mbps") {
				return ErrUnsupportedProjection
			}
		}
	}
	if obfs, ok := raw["obfs"].(map[string]any); ok && unknownKeys(obfs, "type password") {
		return ErrUnsupportedProjection
	}
	return nil
}

func unknownKeys(object map[string]any, fields string) bool {
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

func moreThanOne(value any) bool {
	switch values := value.(type) {
	case []any:
		return len(values) > 1
	case []string:
		return len(values) > 1
	}
	return false
}
