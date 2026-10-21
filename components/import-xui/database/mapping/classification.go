//go:build !minimal

package mapping

import (
	"encoding/json"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/components/import-xui/database/source"
)

const (
	ExactSupported        = "EXACT_SUPPORTED"
	LosslessPreserve      = "LOSSLESS_PRESERVE"
	ConvertWithDiagnostic = "CONVERT_WITH_DIAGNOSTIC"
	ManualMappingRequired = "MANUAL_MAPPING_REQUIRED"
	UnsupportedBlocked    = "UNSUPPORTED_BLOCKED"
	IgnoredExplicitly     = "IGNORED_EXPLICITLY"
)

// FieldClassification contains paths and fixed owner reason codes, never source
// values. The immutable uploaded source remains the authority for manual work.
type FieldClassification struct {
	Path       string `json:"path"`
	State      string `json:"state"`
	ReasonCode string `json:"reasonCode"`
}

func Blocking(fields []FieldClassification) bool {
	for _, field := range fields {
		if field.State == UnsupportedBlocked || field.State == ManualMappingRequired {
			return true
		}
	}
	return false
}

type fieldSchema struct {
	fields  map[string]*fieldSchema
	item    *fieldSchema
	dynamic *fieldSchema
	ignored bool
}

var safeFieldName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]{0,79}$`)

func fieldPath(parent, key string) string {
	if !safeFieldName.MatchString(key) {
		key = "unmapped_field"
	}
	if len(parent) > 512 {
		return "source.depth_limit"
	}
	if parent == "" {
		return key
	}
	return parent + "." + key
}

func schemaFor(t reflect.Type) *fieldSchema {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	schema := &fieldSchema{}
	switch t.Kind() {
	case reflect.Struct:
		schema.fields = map[string]*fieldSchema{}
		for index := 0; index < t.NumField(); index++ {
			field := t.Field(index)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				schema.fields[name] = schemaFor(field.Type)
			}
		}
	case reflect.Slice:
		schema.item = schemaFor(t.Elem())
	}
	return schema
}

func objectSchema(names ...string) *fieldSchema {
	schema := &fieldSchema{fields: map[string]*fieldSchema{}}
	for _, name := range names {
		schema.fields[name] = &fieldSchema{}
	}
	return schema
}

func streamSchema() *fieldSchema {
	schema := schemaFor(reflect.TypeOf(xuiStreamSettings{}))
	schema.fields["tcpSettings"] = objectSchema("header")
	schema.fields["tcpSettings"].fields["header"] = objectSchema("type")
	schema.fields["wsSettings"] = objectSchema("path", "host", "headers")
	schema.fields["wsSettings"].fields["headers"] = &fieldSchema{dynamic: &fieldSchema{}}
	schema.fields["grpcSettings"] = objectSchema("serviceName")
	schema.fields["httpSettings"] = objectSchema("host", "path")
	schema.fields["httpSettings"].fields["host"] = &fieldSchema{item: &fieldSchema{}}
	schema.fields["httpupgradeSettings"] = objectSchema("host", "path")
	return schema
}

func classifyValue(value any, schema *fieldSchema, path string, result *[]FieldClassification) {
	if len(*result) >= 256 {
		if len(*result) == 256 {
			*result = append(*result, FieldClassification{Path: "source", State: UnsupportedBlocked, ReasonCode: "mapping_complexity_limit"})
		}
		return
	}
	if schema == nil {
		*result = append(*result, FieldClassification{Path: path, State: UnsupportedBlocked, ReasonCode: "unmapped_source_field"})
		return
	}
	if schema.ignored {
		*result = append(*result, FieldClassification{Path: path, State: IgnoredExplicitly, ReasonCode: "outside_selected_import_scope"})
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		if schema.fields == nil && schema.dynamic == nil {
			*result = append(*result, FieldClassification{Path: path, State: UnsupportedBlocked, ReasonCode: "unmapped_source_shape"})
			return
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			next := schema.fields[key]
			if next == nil {
				next = schema.dynamic
			}
			classifyValue(typed[key], next, fieldPath(path, key), result)
		}
	case []any:
		if schema.item == nil {
			*result = append(*result, FieldClassification{Path: path, State: UnsupportedBlocked, ReasonCode: "unmapped_source_shape"})
			return
		}
		for index, item := range typed {
			classifyValue(item, schema.item, path+"["+strconv.Itoa(index)+"]", result)
		}
	default:
		*result = append(*result, FieldClassification{Path: path, State: ConvertWithDiagnostic, ReasonCode: "canonical_owner_mapping"})
	}
}

func classifyJSON(raw []byte, schema *fieldSchema, path string, result *[]FieldClassification) any {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		*result = append(*result, FieldClassification{Path: path, State: UnsupportedBlocked, ReasonCode: "invalid_source_json"})
		return nil
	}
	classifyValue(value, schema, path, result)
	return value
}

func ClassifyInbound(row source.InboundRow) []FieldClassification {
	result := []FieldClassification{}
	settingsSchema := schemaFor(reflect.TypeOf(xuiInboundSettings{}))
	if row.Protocol == "wireguard" {
		settingsSchema = schemaFor(reflect.TypeOf(xuiWireguardSettings{}))
	}
	classifyJSON(row.Settings, settingsSchema, "settings", &result)
	classifyJSON(row.StreamSettings, streamSchema(), "stream_settings", &result)
	// Sniffing is a source routing policy, not an inbound credential. It is
	// explicitly excluded rather than silently turned into a local DNS rule.
	classifyJSON(row.Sniffing, &fieldSchema{ignored: true}, "sniffing", &result)
	stream, err := parseStreamSettings(row)
	if err != nil {
		return append(result, FieldClassification{Path: "stream_settings", State: UnsupportedBlocked, ReasonCode: "invalid_source_stream"})
	}
	block := func(path, reason string) {
		result = append(result, FieldClassification{Path: path, State: ManualMappingRequired, ReasonCode: reason})
	}
	switch stream.Network {
	case "", "tcp", "ws", "grpc", "h2", "http", "httpupgrade":
	default:
		block("stream_settings.network", "transport_requires_mapping")
	}
	if header, ok := stream.TCPSettings["header"].(map[string]any); ok {
		if typ, _ := header["type"].(string); typ != "" && typ != "none" {
			block("stream_settings.tcpSettings.header", "tcp_obfuscation_requires_mapping")
		}
	}
	switch stream.Security {
	case "", "none":
	case "tls":
		spec, _, err := ExtractPlainTLS(row)
		if err != nil || spec == nil {
			block("stream_settings.tlsSettings", "tls_material_unavailable")
		}
	case "reality":
		spec, _, err := ExtractReality(row)
		if err != nil || spec == nil || spec.PublicKey == "" {
			block("stream_settings.realitySettings", "reality_material_unavailable")
		}
	default:
		block("stream_settings.security", "security_requires_mapping")
	}
	var settings xuiInboundSettings
	if row.Protocol != "wireguard" && json.Unmarshal(row.Settings, &settings) == nil {
		if settings.Decryption != "" && settings.Decryption != "none" {
			block("settings.decryption", "decryption_requires_mapping")
		}
		if settings.Network != "" {
			block("settings.network", "network_scope_requires_mapping")
		}
		if settings.Encryption != "" && settings.Encryption != "none" {
			block("settings.encryption", "encryption_requires_mapping")
		}
		for index, client := range settings.Clients {
			if client.Security != "" && client.Security != "auto" {
				block("settings.clients["+strconv.Itoa(index)+"].security", "client_cipher_requires_mapping")
			}
		}
	}
	return result
}

func InboundClientNames(row source.InboundRow) []string {
	var settings xuiInboundSettings
	if json.Unmarshal(row.Settings, &settings) != nil {
		return nil
	}
	refs := clientRefsFromSettings(row, inboundType(row.Protocol), settings)
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.Email)
	}
	return names
}

func ClassifyXrayConfig(raw string) []FieldClassification {
	result := []FieldClassification{}
	root := objectSchema("outbounds", "routing", "dns")
	// Xray API/log/inbounds/policy/statistics are never destinations of the
	// current routing import choice. Their exclusion is visible and explicit.
	for _, name := range []string{"log", "api", "inbounds", "policy", "stats", "reverse", "observatory", "burstObservatory"} {
		root.fields[name] = &fieldSchema{ignored: true}
	}
	outbound := schemaFor(reflect.TypeOf(xrayOutbound{}))
	outbound.fields["streamSettings"] = streamSchema()
	outbound.fields["settings"] = &fieldSchema{ignored: true} // classified by the protocol below
	outbound.fields["mux"] = objectSchema("enabled", "concurrency", "xudpConcurrency")
	root.fields["outbounds"] = &fieldSchema{item: outbound}
	root.fields["routing"] = objectSchema("rules", "domainStrategy")
	rule := objectSchema("type", "outboundTag", "domain", "ip", "source", "port", "sourcePort", "network", "protocol", "inboundTag", "user")
	for _, name := range []string{"domain", "ip", "source", "protocol", "inboundTag", "user"} {
		rule.fields[name] = &fieldSchema{item: &fieldSchema{}}
	}
	root.fields["routing"].fields["rules"] = &fieldSchema{item: rule}
	root.fields["dns"] = objectSchema("servers", "queryStrategy", "clientIp", "hosts")
	root.fields["dns"].fields["hosts"] = &fieldSchema{ignored: true}
	root.fields["dns"].fields["servers"] = &fieldSchema{item: objectSchema("address", "domains")}
	root.fields["dns"].fields["servers"].item.fields["domains"] = &fieldSchema{item: &fieldSchema{}}
	classifyJSON([]byte(raw), root, "xrayConfig", &result)
	var config struct {
		Outbounds []xrayOutbound `json:"outbounds"`
	}
	if json.Unmarshal([]byte(raw), &config) != nil {
		return result
	}
	for index, candidate := range config.Outbounds {
		path := "xrayConfig.outbounds[" + strconv.Itoa(index) + "].settings"
		var schema *fieldSchema
		switch candidate.Protocol {
		case "wireguard":
			schema = schemaFor(reflect.TypeOf(xrayWireguardOutbound{}))
		case "vmess", "vless", "trojan", "shadowsocks", "socks", "http":
			schema = schemaFor(reflect.TypeOf(xrayProxySettings{}))
		case "freedom":
			schema = objectSchema("domainStrategy")
		case "blackhole":
			schema = objectSchema("response")
			schema.fields["response"] = objectSchema("type")
		case "dns":
			schema = objectSchema()
		}
		classifyJSON(candidate.Settings, schema, path, &result)
	}
	return result
}
