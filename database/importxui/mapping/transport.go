package mapping

import (
	"fmt"
	"strings"
)

// mapTransport maps an Xray stream's transport to the sing-box transport block.
// It is shared by inbound and outbound mapping; entity ("inbound"/"outbound")
// only labels warning messages so the import report attributes them correctly.
func mapTransport(entity string, tag string, stream xuiStreamSettings) (map[string]any, []string) {
	network := strings.ToLower(strings.TrimSpace(stream.Network))
	switch network {
	case "", "tcp":
		return nil, nil
	case "ws":
		transport := map[string]any{"type": "ws"}
		if path, ok := stringFromMap(stream.WSSettings, "path"); ok && path != "" {
			transport["path"] = path
		}
		if headers := wsHeaders(stream.WSSettings); len(headers) > 0 {
			transport["headers"] = headers
		}
		return transport, nil
	case "grpc":
		transport := map[string]any{"type": "grpc"}
		if serviceName, ok := stringFromMap(stream.GRPCSettings, "serviceName"); ok && serviceName != "" {
			transport["service_name"] = serviceName
		}
		return transport, nil
	case "h2", "http":
		transport := map[string]any{"type": "http"}
		if hosts, ok := stringSliceFromMap(stream.HTTPSettings, "host"); ok {
			transport["host"] = hosts
		}
		if path, ok := stringFromMap(stream.HTTPSettings, "path"); ok && path != "" {
			transport["path"] = path
		}
		return transport, nil
	case "httpupgrade":
		transport := map[string]any{"type": "httpupgrade"}
		if host, ok := stringFromMap(stream.HTTPUPSettings, "host"); ok && host != "" {
			transport["host"] = host
		}
		if path, ok := stringFromMap(stream.HTTPUPSettings, "path"); ok && path != "" {
			transport["path"] = path
		}
		return transport, nil
	case "splithttp", "xhttp":
		transport := map[string]any{"type": "httpupgrade"}
		if path, ok := stringFromMap(stream.HTTPUPSettings, "path"); ok && path != "" {
			transport["path"] = path
		}
		if host, ok := stringFromMap(stream.HTTPUPSettings, "host"); ok && host != "" {
			transport["host"] = host
		}
		return transport, []string{fmt.Sprintf("%s %s: transport %q mapped to httpupgrade; manual review recommended", entity, tag, network)}
	default:
		return nil, []string{fmt.Sprintf("%s %s: transport %q requires manual review", entity, tag, network)}
	}
}

// wsHeaders collects all WebSocket request headers from Xray wsSettings. Xray
// stores custom headers under "headers"; some panels also keep the Host under a
// top-level "host". sing-box carries every header in the transport headers map.
func wsHeaders(ws map[string]any) map[string]any {
	out := map[string]any{}
	if headers, ok := mapFromMap(ws, "headers"); ok {
		for k, v := range headers {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
				out[k] = s
			}
		}
	}
	if _, has := out["Host"]; !has {
		if host, ok := stringFromMap(ws, "host"); ok && host != "" {
			out["Host"] = host
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mapOutboundTLSBlock(stream xuiStreamSettings, reality *RealitySpec) (map[string]any, []string) {
	switch stream.Security {
	case "":
		return nil, nil
	case "none":
		return nil, nil
	case "reality":
		if reality == nil {
			return nil, []string{"reality TLS settings are incomplete; outbound preview has no TLS block"}
		}
		return map[string]any{
			"enabled":     true,
			"server_name": reality.ServerName,
			"utls": map[string]any{
				"enabled":     true,
				"fingerprint": reality.Fingerprint,
			},
			"reality": map[string]any{
				"enabled":    true,
				"public_key": reality.PublicKey,
				"short_id":   firstString(reality.ShortIDs),
			},
		}, nil
	case "tls":
		block := map[string]any{"enabled": true}
		if sni := strings.TrimSpace(stream.TLSSettings.ServerName); sni != "" {
			block["server_name"] = sni
		}
		if stream.TLSSettings.AllowInsecure {
			block["insecure"] = true
		}
		if len(stream.TLSSettings.ALPN) > 0 {
			block["alpn"] = stream.TLSSettings.ALPN
		}
		if fp := strings.TrimSpace(stream.TLSSettings.Fingerprint); fp != "" {
			block["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
		}
		return block, nil
	default:
		return nil, []string{fmt.Sprintf("TLS security %q requires manual review", stream.Security)}
	}
}

// mapOutboundClientTLS builds the sing-box outbound tls block from an Xray
// outbound stream. For reality it uses the peer public key/short id stored at
// the top level of realitySettings.
func mapOutboundClientTLS(tag string, stream xuiStreamSettings) (map[string]any, []string) {
	switch stream.Security {
	case "", "none":
		return nil, nil
	case "tls":
		tls := map[string]any{"enabled": true}
		if sni := strings.TrimSpace(stream.TLSSettings.ServerName); sni != "" {
			tls["server_name"] = sni
		}
		if stream.TLSSettings.AllowInsecure {
			tls["insecure"] = true
		}
		if len(stream.TLSSettings.ALPN) > 0 {
			tls["alpn"] = stream.TLSSettings.ALPN
		}
		if fp := strings.TrimSpace(stream.TLSSettings.Fingerprint); fp != "" {
			tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
		}
		return tls, nil
	case "reality":
		r := stream.RealitySettings
		serverName := firstNonEmpty(r.ServerName, firstString(r.ServerNames))
		fingerprint := firstNonEmpty(r.Fingerprint, "chrome")
		var warnings []string
		if strings.TrimSpace(r.PublicKey) == "" {
			warnings = append(warnings, fmt.Sprintf("outbound %s: reality publicKey is empty; verify the outbound TLS settings", tag))
		}
		tls := map[string]any{
			"enabled":     true,
			"server_name": serverName,
			"utls": map[string]any{
				"enabled":     true,
				"fingerprint": fingerprint,
			},
			"reality": map[string]any{
				"enabled":    true,
				"public_key": strings.TrimSpace(r.PublicKey),
				"short_id":   firstNonEmpty(r.ShortID, firstString(r.ShortIDs)),
			},
		}
		return tls, warnings
	default:
		return nil, []string{fmt.Sprintf("outbound %s: TLS security %q requires manual review", tag, stream.Security)}
	}
}

func stringFromMap(values map[string]any, key string) (string, bool) {
	if values == nil {
		return "", false
	}
	value, ok := values[key]
	if !ok {
		return "", false
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v), true
	default:
		return strings.TrimSpace(fmt.Sprint(v)), true
	}
}

func mapFromMap(values map[string]any, key string) (map[string]any, bool) {
	if values == nil {
		return nil, false
	}
	value, ok := values[key]
	if !ok {
		return nil, false
	}
	casted, ok := value.(map[string]any)
	return casted, ok
}

func stringSliceFromMap(values map[string]any, key string) ([]string, bool) {
	if values == nil {
		return nil, false
	}
	value, ok := values[key]
	if !ok {
		return nil, false
	}
	switch v := value.(type) {
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				result = append(result, text)
			}
		}
		return result, len(result) > 0
	case []string:
		return v, len(v) > 0
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, false
		}
		return []string{strings.TrimSpace(v)}, true
	default:
		text := strings.TrimSpace(fmt.Sprint(v))
		if text == "" {
			return nil, false
		}
		return []string{text}, true
	}
}
