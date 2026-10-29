package canonical

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	clientfacts "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/clientfacts"
	entityprotocol "github.com/MalenkiySolovey/solovey-ui/internal/entities/protocol"
)

const (
	ProjectionExact       = "EXACT"
	ProjectionAdapted     = "SUPPORTED_WITH_APPROVED_ADAPTATION"
	ProjectionManual      = "MANUAL_MAPPING_REQUIRED"
	ProjectionUnsupported = "UNSUPPORTED"
)

// Authentication keeps the protocol's existing credential fields. It does not
// create another credential resource or change their current secret owner.
type Authentication struct {
	Protocol string         `json:"protocol,omitempty"`
	Fields   map[string]any `json:"fields,omitempty"`
}

// ProjectionDiagnostic is safe to display. Codes and paths are owner constants;
// source URLs, authentication values and native payloads never enter errors.
type ProjectionDiagnostic struct {
	State string `json:"state"`
	Code  string `json:"code"`
	Field string `json:"field"`
}

var ErrEndpointMapping = errors.New("MANUAL_MAPPING_REQUIRED: endpoint semantics cannot be projected safely")

var authenticationFields = []string{
	"uuid", "username", "user", "password", "method", "flow", "alter_id", "security",
	"auth", "auth_str", "token", "private_key", "private_key_path", "private_key_passphrase",
	"key",
}

// NormalizeEndpoint evolves the existing observation model into EndpointIR.
// The existing captured payload remains the source for legacy snapshots. The
// normalized fields are derived, never an independently editable database.
func NormalizeEndpoint(connection Connection) EndpointIR {
	raw := CleanOutbound(connection.BestOutbound)
	if len(raw) == 0 {
		raw = map[string]any{"type": connection.Protocol, "tag": connection.DisplayName}
		if connection.Endpoint.Server != "" {
			raw["server"] = connection.Endpoint.Server
		}
		if connection.Endpoint.Port != "" {
			raw["server_port"] = connection.Endpoint.Port
		}
	}
	if _, ok := raw["type"]; !ok && connection.Protocol != "" {
		raw["type"] = connection.Protocol
	}
	if _, ok := raw["server"]; !ok && connection.Endpoint.Server != "" {
		raw["server"] = connection.Endpoint.Server
	}
	if _, ok := raw["server_port"]; !ok && connection.Endpoint.Port != "" {
		raw["server_port"] = connection.Endpoint.Port
	}
	connection.Protocol = strings.TrimSpace(stringValue(raw["type"]))
	connection.Endpoint = Endpoint{Server: strings.TrimSpace(stringValue(raw["server"])), Port: portString(raw["server_port"])}
	connection.Authentication = Authentication{Protocol: connection.Protocol, Fields: map[string]any{}}
	for _, key := range authenticationFields {
		if value, present := raw[key]; present {
			connection.Authentication.Fields[key] = cloneValue(value)
		}
	}
	connection.TLS = tlsInfo(raw)
	connection.TLS.Options = mapValue(raw["tls"])
	connection.Transport = transportInfo(raw)
	connection.Transport.Options = mapValue(raw["transport"])
	connection.Multiplex = mapValue(raw["multiplex"])
	connection.UDP = clientfacts.SupportsUDP(connection.Protocol, raw)
	connection.Diagnostics = criticalShapeDiagnostics(raw)
	connection.Projection = ProjectionExact
	for _, observation := range connection.Observations {
		connection.Diagnostics = append(connection.Diagnostics, observation.Diagnostics...)
	}
	// Group adaptations stay with the existing declared conversion policy.
	// This boundary never changes an endpoint's network protocol.
	if connection.Kind != KindGroup && len(connection.GroupMembers) == 0 {
		first, _ := json.Marshal(semanticEndpoint(raw))
		for _, observation := range connection.Observations {
			if len(observation.Outbound) == 0 {
				continue
			}
			next, _ := json.Marshal(semanticEndpoint(observation.Outbound))
			if string(first) != string(next) {
				connection.Diagnostics = append(connection.Diagnostics, ProjectionDiagnostic{State: ProjectionManual, Code: "endpoint_observations_conflict", Field: "endpoint"})
				break
			}
		}
		for _, adaptation := range connection.Adaptations {
			if adaptation.SourceFeature == "streamSettings" && adaptation.SourceType != adaptation.TargetType {
				connection.Diagnostics = append(connection.Diagnostics, ProjectionDiagnostic{State: ProjectionUnsupported, Code: "transport_substitution_forbidden", Field: "transport"})
				break
			}
		}
	}
	if len(connection.Diagnostics) > 0 {
		connection.Projection = connection.Diagnostics[0].State
		for _, diagnostic := range connection.Diagnostics {
			if diagnostic.State == ProjectionUnsupported {
				connection.Projection = ProjectionUnsupported
				break
			}
		}
	}
	stable := semanticEndpoint(raw)
	// semanticEndpoint includes preserved protocol-specific options while
	// excluding serializer metadata, public labels and provider URLs.
	encoded, _ := json.Marshal(stable)
	digest := sha256.Sum256(encoded)
	connection.EndpointID = "endpoint:" + hex.EncodeToString(digest[:24])
	return connection
}

func mapValue(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return cloneMap(object)
	}
	return nil
}

func semanticOptions(raw map[string]any) map[string]any {
	copy := CleanOutbound(raw)
	for _, key := range []string{"tag", "name", "remark", "remarks", "ps", "id", "sortOrder", "sort_order"} {
		delete(copy, key)
	}
	return copy
}

func semanticEndpoint(raw map[string]any) map[string]any {
	auth := map[string]any{}
	for _, key := range authenticationFields {
		if value, present := raw[key]; present && value != nil {
			if text, ok := value.(string); ok && text == "" {
				continue
			}
			auth[key] = cloneValue(value)
		}
	}
	// These are the protocol defaults already used by current parsers.
	if stringValue(raw["type"]) == "vmess" {
		if auth["security"] == "auto" {
			delete(auth, "security")
		}
		if portString(auth["alter_id"]) == "0" {
			delete(auth, "alter_id")
		}
	}
	tls := mapValue(raw["tls"])
	if len(tls) == 0 || (len(tls) == 1 && tls["enabled"] == false) {
		tls = nil
	}
	if tls != nil {
		if value, ok := tls["insecure"].(bool); ok && !value {
			delete(tls, "insecure")
		}
		// Explicitly disabled owner features are passive configuration. They
		// remain in BestOutbound/provenance but do not identify the active
		// endpoint when a target omits them. A later enable changes identity.
		for _, feature := range []string{"utls", "reality"} {
			if options, ok := tls[feature].(map[string]any); ok && options["enabled"] == false {
				delete(tls, feature)
			}
		}
	}
	transport := mapValue(raw["transport"])
	if transport["type"] == "xhttp" {
		if mode := transport["mode"]; mode == nil || mode == "" || mode == "auto" {
			delete(transport, "mode")
		}
		if transport["x_padding_bytes"] == entityprotocol.XHTTPDefaultPadding {
			delete(transport, "x_padding_bytes")
		}
	}
	if stringValue(transport["type"]) == "ws" && (transport["max_early_data"] == nil || portString(transport["max_early_data"]) == "0") {
		delete(transport, "early_data_header_name")
		delete(transport, "max_early_data")
	}
	if len(transport) == 0 || (len(transport) == 1 && transport["type"] == "tcp") {
		transport = nil
	}
	multiplex := mapValue(raw["multiplex"])
	if len(multiplex) == 1 && multiplex["enabled"] == false {
		multiplex = nil
	}
	options := semanticOptions(raw)
	if raw["type"] == "sudoku" {
		// Native SDK defaults are semantic equivalents, including the omitted
		// compact-link flags. They do not rewrite the captured source payload.
		if value := stringValue(options["aead_method"]); value == "" {
			options["aead_method"] = "chacha20-poly1305"
		}
		switch stringValue(options["table_type"]) {
		case "", "entropy":
			options["table_type"] = "prefer_entropy"
		case "ascii":
			options["table_type"] = "prefer_ascii"
		}
		if options["enable_pure_downlink"] == nil {
			options["enable_pure_downlink"] = true
		}
		mask := mapValue(options["http_mask"])
		if mask == nil {
			mask = map[string]any{"enabled": true}
		}
		if mask["enabled"] == nil {
			mask["enabled"] = false
		}
		if stringValue(mask["mode"]) == "" {
			mask["mode"] = "legacy"
		}
		if stringValue(mask["path_root"]) == "" {
			delete(mask, "path_root")
		}
		options["http_mask"] = mask
		if options["padding_min"] == nil {
			options["padding_min"] = 10
		}
		if options["padding_max"] == nil {
			options["padding_max"] = 30
		}
	}
	for _, key := range []string{"type", "server", "server_port", "tls", "transport", "multiplex", "obfs", "plugin", "plugin_opts"} {
		if (key == "tls" || key == "transport" || key == "multiplex") && raw[key] != nil && mapValue(raw[key]) == nil {
			continue // Keep malformed critical carriers in semantic identity too.
		}
		delete(options, key)
	}
	for _, key := range authenticationFields {
		delete(options, key)
	}
	return map[string]any{"protocol": strings.TrimSpace(stringValue(raw["type"])),
		"server": strings.ToLower(strings.TrimSpace(stringValue(raw["server"]))),
		"port":   portString(raw["server_port"]), "authentication": auth,
		"tls": tls, "transport": transport, "multiplex": multiplex,
		"obfs": cloneValue(raw["obfs"]), "plugin": cloneValue(raw["plugin"]), "plugin_opts": cloneValue(raw["plugin_opts"]), "protocolOptions": options}
}

// EndpointOutbound materializes normalized semantic fields while preserving
// protocol-specific options. It is a representation operation, never a save.
func EndpointOutbound(endpoint EndpointIR) map[string]any {
	raw := CleanOutbound(endpoint.BestOutbound)
	if raw == nil {
		raw = map[string]any{}
	}
	raw["type"] = endpoint.Protocol
	if endpoint.DisplayName != "" {
		raw["tag"] = endpoint.DisplayName
	}
	if endpoint.Endpoint.Server != "" {
		raw["server"] = endpoint.Endpoint.Server
	}
	if endpoint.Endpoint.Port != "" && portString(raw["server_port"]) != endpoint.Endpoint.Port {
		raw["server_port"] = endpoint.Endpoint.Port
	}
	for _, key := range authenticationFields {
		delete(raw, key)
	}
	for key, value := range endpoint.Authentication.Fields {
		raw[key] = cloneValue(value)
	}
	if endpoint.TLS.Options != nil {
		raw["tls"] = cloneMap(endpoint.TLS.Options)
	}
	if endpoint.Transport.Options != nil {
		raw["transport"] = cloneMap(endpoint.Transport.Options)
	}
	if endpoint.Multiplex != nil {
		raw["multiplex"] = cloneMap(endpoint.Multiplex)
	}
	return raw
}

func CheckedEndpointOutbound(connection Connection) (map[string]any, error) {
	endpoint := NormalizeEndpoint(connection)
	if endpoint.Projection == ProjectionManual || endpoint.Projection == ProjectionUnsupported {
		return nil, ErrEndpointMapping
	}
	return EndpointOutbound(endpoint), nil
}
