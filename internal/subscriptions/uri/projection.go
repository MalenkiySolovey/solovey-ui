package uri

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/uri/codec"
)

// UnsupportedProjection is a delivery limitation, not permission to reject a
// valid runtime configuration. Client-link owners surface it without a URI.
type UnsupportedProjection struct{ Reason string }

func (e *UnsupportedProjection) Error() string { return e.Reason }

func decodeOutboundProjection(inbound map[string]any) (map[string]any, error) {
	value := inbound["out_json"]
	if value == nil {
		return map[string]any{}, nil
	}
	if object, ok := value.(map[string]any); ok {
		return object, nil
	}
	raw, ok := value.(json.RawMessage)
	if !ok {
		return nil, errors.New("invalid subscription outbound metadata")
	}
	if len(strings.TrimSpace(string(raw))) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return map[string]any{}, nil
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, errors.New("subscription outbound metadata must be an object")
	}
	return object, nil
}

func validateProjection(typ string, inbound map[string]any, addresses []map[string]any) error {
	outbound, err := decodeOutboundProjection(inbound)
	if err != nil {
		return err
	}
	switch typ {
	case "shadowsocks":
		if _, _, err := canonical.ShadowsocksPlugin(outbound); err != nil {
			return err
		}
	case "hysteria", "hysteria2":
		if _, err := canonical.HysteriaPorts(outbound["server_ports"]); err != nil {
			return err
		}
		if interval, present := outbound["hop_interval"]; present && interval != nil && interval != "" && interval != "0s" {
			return &UnsupportedProjection{Reason: "This Hysteria URI format cannot preserve hop_interval. Use the JSON subscription for the configured interval."}
		}
	case "naive":
		if _, err := naiveSchemes(inbound["network"]); err != nil {
			return err
		}
		for _, addr := range addresses {
			tls, _ := addr["tls"].(map[string]any)
			if len(tlsStringList(tls["certificate_public_key_sha256"])) > 0 || len(tlsStringList(tls["certificate"])) > 0 {
				return &UnsupportedProjection{Reason: "Naive share links cannot preserve certificate pins or a custom trust source. Use a client profile with the configured certificate trust."}
			}
		}
	}
	if transport, ok := inbound["transport"].(map[string]any); ok && transport["type"] == "ws" {
		if _, err := codec.WebSocketPath(transport); err != nil {
			if errors.Is(err, codec.ErrUnsupportedWebSocketHeader) {
				return &UnsupportedProjection{Reason: "This URI format supports WebSocket early data only with Sec-WebSocket-Protocol. Use JSON for the configured header."}
			}
			return err
		}
	}
	return nil
}

func linkURL(scheme string, user *url.Userinfo, address map[string]any, params []LinkParam, remark string) string {
	port, _ := address["server_port"].(float64)
	authority, err := codec.Authority(mapString(address, "server"), uint16(port))
	if err != nil {
		return ""
	}
	result := &url.URL{Scheme: scheme, User: user, Host: authority, Fragment: remark}
	result.RawQuery = encodeParams(params)
	return result.String()
}

func encodeParams(params []LinkParam) string {
	query := url.Values{}
	for _, param := range params {
		query.Set(param.Key, param.Value)
	}
	parts := strings.Split(query.Encode(), "&")
	for i, part := range parts {
		if strings.HasPrefix(part, "alpn=") || strings.HasPrefix(part, "mport=") {
			parts[i] = strings.ReplaceAll(part, "%2C", ",")
		}
		if strings.HasPrefix(part, "mport=") {
			parts[i] = strings.ReplaceAll(parts[i], "%3A", ":")
		}
	}
	return strings.Join(parts, "&")
}

func portHoppingParam(inbound map[string]any) string {
	outbound, err := decodeOutboundProjection(inbound)
	if err != nil {
		return ""
	} // Generate validates before calling helpers.
	ports, _ := canonical.HysteriaSharePorts(outbound["server_ports"])
	return ports
}

func naiveSchemes(network any) ([]string, error) {
	var networks []string
	switch typed := network.(type) {
	case nil:
		networks = []string{"tcp", "udp"}
	case string:
		if typed == "" {
			networks = []string{"tcp", "udp"}
		} else {
			networks = []string{typed}
		}
	case []string:
		networks = typed
	case []any:
		for _, value := range typed {
			text, ok := value.(string)
			if !ok {
				return nil, errors.New("invalid Naive network")
			}
			networks = append(networks, text)
		}
	default:
		return nil, errors.New("invalid Naive network")
	}
	if len(networks) == 0 {
		networks = []string{"tcp", "udp"}
	}
	var tcp, quic bool
	for _, network := range networks {
		switch network {
		case "tcp":
			tcp = true
		case "udp":
			quic = true
		default:
			return nil, errors.New("invalid Naive network")
		}
	}
	var schemes []string
	if tcp {
		schemes = append(schemes, "http2", "naive+https")
	}
	if quic {
		schemes = append(schemes, "naive+quic")
	}
	return schemes, nil
}
