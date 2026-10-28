package protocol

import (
	"encoding/json"
	"reflect"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/MalenkiySolovey/solovey-ui/util/jsonfields"
	"github.com/sagernet/sing-box/option"
	sbjson "github.com/sagernet/sing/common/json"
)

type TransportEditorFact struct {
	registry.Fact
	Available bool                `json:"available"`
	Fields    map[string][]string `json:"fields,omitempty"`
	Defaults  map[string]any      `json:"defaults,omitempty"`
	Choices   map[string][]string `json:"choices,omitempty"`
}

func transportEditorFacts() map[string]TransportEditorFact {
	result := map[string]TransportEditorFact{}
	for _, fact := range registry.TransportFacts() {
		item := TransportEditorFact{Fact: fact, Available: fact.Available()}
		if fact.Type == "mkcp" && fact.Registered {
			if field, found := reflect.TypeFor[option.V2RayTransportOptions]().FieldByName("KCPOptions"); found {
				item.Fields = map[string][]string{"root": jsonfields.Names(field.Type, false)}
			}
			item.Choices = map[string][]string{"header_type": {"none", "srtp", "utp", "wechat-video", "dtls", "wireguard"}}
		}
		if fact.Type == "xhttp" && fact.Registered {
			item.Fields = map[string][]string{}
			if field, found := reflect.TypeFor[option.V2RayTransportOptions]().FieldByName("XHTTPOptions"); found {
				item.Fields["root"] = jsonfields.Names(field.Type, false)
				for _, name := range []string{"Download", "Xmux"} {
					if nested, found := field.Type.FieldByName(name); found {
						typ := nested.Type
						for typ.Kind() == reflect.Pointer {
							typ = typ.Elem()
						}
						item.Fields[strings.ToLower(name)] = jsonfields.Names(typ, false)
					}
				}
			}
			// New drafts use the stable target's explicit valid padding range.
			// Existing stored drafts are never rewritten by editor fact loading.
			item.Defaults = map[string]any{"mode": "auto", "x_padding_bytes": "100-1000"}
			item.Choices = map[string][]string{}
			for name, values := range map[string][]string{
				"mode":                  {"auto", "packet-up", "stream-up", "stream-one"},
				"domain_strategy":       {"prefer_ipv4", "prefer_ipv6", "ipv4_only", "ipv6_only"},
				"x_padding_placement":   {"queryInHeader", "cookie", "header", "query"},
				"x_padding_method":      {"repeat-x", "tokenish"},
				"session_placement":     {"path", "cookie", "header", "query"},
				"seq_placement":         {"path", "cookie", "header", "query"},
				"uplink_http_method":    {"POST", "GET"},
				"uplink_data_placement": {"auto", "body", "cookie", "header"},
			} {
				for _, value := range values {
					probe := map[string]any{"type": "xhttp", "mode": "packet-up", "x_padding_bytes": "100-1000", name: value}
					raw, _ := json.Marshal(probe)
					var native option.V2RayTransportOptions
					if sbjson.Unmarshal(raw, &native) == nil {
						item.Choices[name] = append(item.Choices[name], value)
					}
				}
			}
		}
		result[fact.Type] = item
	}
	return result
}

// TransportFindings validates the nested native consumer without normalizing
// canonical JSON or echoing private paths, headers or credential material.
func TransportFindings(kind, side, path string, source json.RawMessage) []diagnostics.Finding {
	// Other protocols use the same JSON name for their own transport enums.
	if kind != "vmess" && kind != "vless" && kind != "trojan" {
		return nil
	}
	var parent map[string]json.RawMessage
	if json.Unmarshal(source, &parent) != nil {
		return nil
	}
	for key := range parent {
		if key != "transport" && strings.EqualFold(key, "transport") {
			return []diagnostics.Finding{finding(path+".transport", "TRANSPORT_OPTIONS_INVALID", diagnostics.Error, "Use the canonical transport field.")}
		}
	}
	raw, present := parent["transport"]
	if !present || string(raw) == "null" {
		return nil
	}
	var fields map[string]json.RawMessage
	if jsonvalue.StrictObject(raw) != nil || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return []diagnostics.Finding{finding(path+".transport", "TRANSPORT_OPTIONS_INVALID", diagnostics.Error, "Transport options must be an object.")}
	}
	if len(fields) == 0 {
		return nil
	}
	var name string
	if json.Unmarshal(fields["type"], &name) != nil || name == "" {
		if len(fields) == 1 && string(fields["type"]) == `""` {
			return nil
		}
		return []diagnostics.Finding{finding(path+".transport", "TRANSPORT_TYPE_REQUIRED", diagnostics.Error, "Transport options require an explicit native type.")}
	}
	if !registry.ResolveTransport(name).Available() {
		return []diagnostics.Finding{finding(path+".transport", "TRANSPORT_UNAVAILABLE", diagnostics.Error, "The selected runtime cannot apply this transport; its stored options must be preserved.")}
	}
	if name == "xhttp" && side == "inbound" {
		if _, present := fields["download"]; present {
			return []diagnostics.Finding{finding(path+".transport.download", "TRANSPORT_DOWNLOAD_OUTBOUND_ONLY", diagnostics.Error, "A separate download leg belongs to the outbound client.")}
		}
	}
	if name == "xhttp" {
		contract := transportEditorFacts()["xhttp"]
		if !transportObjectKeys(fields, append([]string{"type"}, contract.Fields["root"]...)) {
			return []diagnostics.Finding{finding(path+".transport", "TRANSPORT_OPTIONS_INVALID", diagnostics.Error, "Transport fields must use the selected native schema and canonical names.")}
		}
		for _, name := range []string{"download", "xmux"} {
			if value, present := fields[name]; present && string(value) != "null" {
				var nested map[string]json.RawMessage
				if json.Unmarshal(value, &nested) != nil || !transportObjectKeys(nested, contract.Fields[name]) {
					return []diagnostics.Finding{finding(path+".transport."+name, "TRANSPORT_OPTIONS_INVALID", diagnostics.Error, "Transport fields must use the selected native schema and canonical names.")}
				}
				if name == "download" {
					if tls, present := nested["tls"]; present {
						if findings := entitytls.TLSOptionsFindings("client", path+".transport.download.tls", tls); len(findings) != 0 {
							return findings
						}
					}
				}
			}
		}
	}
	consumerRaw := raw
	if name == "mkcp" {
		if !transportObjectKeys(fields, append([]string{"type"}, transportEditorFacts()[name].Fields["root"]...)) {
			return []diagnostics.Finding{finding(path+".transport", "TRANSPORT_OPTIONS_INVALID", diagnostics.Error, "Transport fields must use the selected native schema and canonical names.")}
		}
		if seed, present := fields["seed"]; present {
			var value string
			if json.Unmarshal(seed, &value) != nil {
				var mutation struct{ Intent string }
				if jsonvalue.StrictObject(seed) != nil || json.Unmarshal(seed, &mutation) != nil || (mutation.Intent != "replace" && mutation.Intent != "unchanged" && mutation.Intent != "clear") {
					return []diagnostics.Finding{finding(path+".transport.seed", "TRANSPORT_CREDENTIAL_INVALID", diagnostics.Error, "Use the owning transport credential field.")}
				}
				// Only the isolated schema probe substitutes this value. The save
				// owner validates the actual row-bound mutation before persistence;
				// the complete candidate uses its borrowed plaintext afterward.
				fields["seed"] = json.RawMessage(`"owner-validation"`)
				consumerRaw, _ = json.Marshal(fields)
			}
		}
	}
	var native option.V2RayTransportOptions
	if sbjson.Unmarshal(consumerRaw, &native) != nil {
		return []diagnostics.Finding{finding(path+".transport", "TRANSPORT_OPTIONS_INVALID", diagnostics.Error, "Transport fields or combinations are rejected by the selected native consumer.")}
	}
	return nil
}

func transportObjectKeys(fields map[string]json.RawMessage, names []string) bool {
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = true
	}
	for name := range fields {
		if !allowed[name] {
			return false
		}
	}
	return fields != nil
}
