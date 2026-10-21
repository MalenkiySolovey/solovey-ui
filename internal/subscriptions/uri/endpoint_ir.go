package uri

import (
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri/codec"
	"net/url"
	"strings"
)

// generateEndpointLinks adapts the existing owner inputs into the same IR used
// for external activation. Protocol encoders remain the current URI adapters.
func generateEndpointLinks(protocol string, credential, inbound map[string]any, addresses []map[string]any) ([]string, error) {
	base, err := decodeOutboundProjection(inbound)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, address := range addresses {
		raw := map[string]any{}
		for key, value := range base {
			raw[key] = value
		}
		raw["type"], raw["tag"] = protocol, address["remark"]
		raw["server"], raw["server_port"] = address["server"], address["server_port"]
		if transport := inbound["transport"]; transport != nil {
			raw["transport"] = transport
		} else {
			delete(raw, "transport")
		}
		if tls := address["tls"]; tls != nil {
			raw["tls"] = tls
		} else {
			delete(raw, "tls")
		}
		keys := []string{"password"}
		if protocol == "vless" {
			keys = []string{"uuid", "flow"}
		}
		if protocol == "vmess" {
			keys = []string{"uuid", "security", "alter_id"}
		}
		for _, key := range keys {
			if value, present := credential[key]; present {
				raw[key] = value
			}
		}
		// Preserve the current inbound owner's eligibility rule for Vision.
		// Arbitrary IR callers instead receive an explicit projection denial.
		transport, _ := raw["transport"].(map[string]any)
		tls, _ := raw["tls"].(map[string]any)
		if protocol == "vless" && (!asBool(tls["enabled"]) || (mapString(transport, "type") != "" && mapString(transport, "type") != "tcp")) {
			delete(raw, "flow")
		}
		endpoint := canonical.ObserveOutbound(canonical.FormatSingBox, raw, 0)
		links, err := RenderEndpoint(endpoint)
		if err != nil {
			return nil, err
		}
		result = append(result, links...)
	}
	return result, nil
}

// RenderEndpoint consumes normalized facts; it never changes a protocol to
// obtain a share link. Unsupported options require another representation.
func RenderEndpoint(endpoint canonical.EndpointIR) ([]string, error) {
	endpoint = canonical.NormalizeEndpoint(endpoint)
	raw, err := canonical.CheckedEndpointOutbound(endpoint)
	if err != nil {
		return nil, err
	}
	if err := canonical.ValidateEndpointProjection(endpoint, canonical.FormatURI); err != nil {
		return nil, err
	}
	// Existing encoders consume JSON-decoded maps (numbers and listables).
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return nil, err
	}
	address := map[string]any{"server": raw["server"], "server_port": raw["server_port"], "remark": endpoint.DisplayName}
	if tls := raw["tls"]; tls != nil {
		address["tls"] = tls
	}
	inbound := map[string]any{"transport": raw["transport"], "out_json": raw}
	addresses := []map[string]any{address}
	if err := validateProjection(endpoint.Protocol, inbound, addresses); err != nil {
		return nil, err
	}
	var links []string
	switch endpoint.Protocol {
	case "vless":
		transport, _ := raw["transport"].(map[string]any)
		tls, _ := raw["tls"].(map[string]any)
		if mapString(raw, "flow") != "" && (!asBool(tls["enabled"]) || (mapString(transport, "type") != "" && mapString(transport, "type") != "tcp")) {
			return nil, canonical.ErrUnsupportedProjection
		}
		links = vlessLink(raw, inbound, addresses)
	case "trojan":
		links = trojanLink(raw, inbound, addresses)
	case "vmess":
		links = vmessLink(raw, inbound, addresses)
	case "anytls":
		links = anytlsLink(raw, addresses)
	default:
		return nil, canonical.ErrUnsupportedProjection
	}
	for _, link := range links {
		if err := validateGeneratedLink(link); err != nil {
			return nil, err
		}
	}
	return links, nil
}

func classifyLinkSource(link string) []canonical.ProjectionDiagnostic {
	block := func(field string) []canonical.ProjectionDiagnostic {
		return []canonical.ProjectionDiagnostic{{State: canonical.ProjectionManual, Code: "source_fields_unmapped", Field: field}}
	}
	parsed, err := url.Parse(link)
	if err != nil {
		return block("endpoint")
	}
	// VMess payload classification occurs on its decoded native object.
	if strings.EqualFold(parsed.Scheme, "vmess") {
		return classifyVMessSource(link)
	}
	// Match fields actually consumed by the existing protocol parser. A known
	// field for another protocol must not become an ignored critical option.
	tlsFields := "security sni alpn fp insecure allowInsecure disable_sni pinSHA256 pbk sid ech"
	known := ""
	switch strings.ToLower(parsed.Scheme) {
	case "vless":
		known = tlsFields + " type host path serviceName headerType encryption flow"
	case "trojan":
		known = tlsFields + " type host path serviceName headerType"
	case "anytls":
		known = tlsFields
	case "tuic":
		known = tlsFields + " congestion_control udp_relay_mode"
	case "hy", "hysteria":
		known = tlsFields + " obfs obfsParam auth mport downmbps upmbps recv_window_conn recv_window"
	case "hy2", "hysteria2":
		known = tlsFields + " obfs obfs-password mport downmbps upmbps fastopen"
	case "ss", "shadowsocks":
		known = "type host security plugin"
	case "naive+https", "naive+quic", "http2":
		known = "peer insecure alpn tfo"
	}
	fields := map[string]bool{}
	for _, key := range strings.Fields(known) {
		fields[key] = true
	}
	for key := range parsed.Query() {
		if !fields[key] {
			return block("endpoint")
		}
	}
	if security := parsed.Query().Get("security"); security != "" && security != "none" && security != "tls" && security != "reality" {
		return block("tls")
	}
	if encryption := parsed.Query().Get("encryption"); encryption != "" && encryption != "none" {
		return block("authentication")
	}
	if typ := parsed.Query().Get("type"); typ != "" && typ != "tcp" && typ != "ws" && typ != "http" && typ != "h2" && typ != "grpc" && typ != "httpupgrade" {
		if !(typ == "quic" && (parsed.Scheme == "ss" || parsed.Scheme == "shadowsocks")) {
			return block("transport")
		}
	}
	if mode := parsed.Query().Get("mode"); mode != "" {
		return block("transport")
	}
	if header := parsed.Query().Get("headerType"); header != "" && header != "none" && header != "http" {
		return block("transport")
	}
	// Unknown numeric values must not quietly turn critical flags off.
	for _, key := range []string{"insecure", "allowInsecure", "disable_sni"} {
		if value := parsed.Query().Get(key); value != "" && value != "0" && value != "1" && value != "true" && value != "false" {
			return block("tls")
		}
	}
	return nil
}

func classifyVMessSource(link string) []canonical.ProjectionDiagnostic {
	_, payload, _ := strings.Cut(link, "://")
	decoded, err := codec.Decode(payload)
	var data map[string]any
	if err == nil {
		err = json.Unmarshal(decoded, &data)
	}
	if err != nil {
		return []canonical.ProjectionDiagnostic{{State: canonical.ProjectionManual, Code: "source_fields_unmapped", Field: "endpoint"}}
	}
	known := map[string]bool{}
	for _, key := range strings.Fields("v ps add port id aid scy net type host path tls sni alpn fp allowInsecure") {
		known[key] = true
	}
	for key := range data {
		if !known[key] {
			return []canonical.ProjectionDiagnostic{{State: canonical.ProjectionManual, Code: "source_fields_unmapped", Field: "endpoint"}}
		}
	}
	for _, key := range strings.Fields("id scy net type host path tls sni alpn fp") {
		if value, present := data[key]; present {
			if _, ok := value.(string); !ok {
				return []canonical.ProjectionDiagnostic{{State: canonical.ProjectionManual, Code: "source_field_shape_unmapped", Field: "endpoint"}}
			}
		}
	}
	if value, present := data["allowInsecure"]; present {
		valid := false
		switch value := value.(type) {
		case bool:
			valid = true
		case float64:
			valid = value == 0 || value == 1
		case string:
			valid = value == "0" || value == "1" || value == "true" || value == "false" || value == ""
		}
		if !valid {
			return []canonical.ProjectionDiagnostic{{State: canonical.ProjectionManual, Code: "source_field_shape_unmapped", Field: "tls"}}
		}
	}
	if tls := mapString(data, "tls"); tls != "" && tls != "none" && tls != "tls" {
		return []canonical.ProjectionDiagnostic{{State: canonical.ProjectionManual, Code: "source_security_unmapped", Field: "tls"}}
	}
	return nil
}
