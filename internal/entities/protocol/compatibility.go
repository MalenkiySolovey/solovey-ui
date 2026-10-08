// Package protocol owns protocol-specific stored option compatibility. It has
// no database, migration journal, runtime or frontend lifecycle.
package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/MalenkiySolovey/solovey-ui/util/jsonfields"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/byteformats"
	sbjson "github.com/sagernet/sing/common/json"
)

// CurrentFindings selects the actual protocol/side schema. Inbound user DTOs
// are resolved by their own owner, so they are not interpreted as core users.
func CurrentFindings(kind, side, path string, source json.RawMessage) []diagnostics.Finding {
	var consumer any
	switch kind {
	case "hysteria":
		if side == "inbound" {
			consumer = &option.HysteriaInboundOptions{}
		} else {
			consumer = &option.HysteriaOutboundOptions{}
		}
	case "hysteria2":
		if side == "inbound" {
			consumer = &option.Hysteria2InboundOptions{}
		} else {
			consumer = &option.Hysteria2OutboundOptions{}
		}
	case "naive":
		if side == "inbound" {
			consumer = &option.NaiveInboundOptions{}
		} else {
			consumer = &option.NaiveOutboundOptions{}
		}
	default:
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(source, &fields) != nil || fields == nil {
		return []diagnostics.Finding{finding(path, "PROTOCOL_OPTIONS_INVALID", diagnostics.Error, "Protocol options must be an object.")}
	}
	delete(fields, "users")
	delete(fields, "type")
	delete(fields, "tag")
	// The pinned MemoryBytes decoder converts a negative JSON integer to
	// uint64. Reject that wrapping input only at real byte-size consumers;
	// zero, unit strings and unrelated protocol fields retain their semantics.
	var memoryFindings []diagnostics.Finding
	for _, name := range jsonfields.NamesOfType(reflect.TypeOf(consumer).Elem(), reflect.TypeFor[byteformats.MemoryBytes]()) {
		if value, err := strconv.ParseFloat(string(fields[name]), 64); err == nil && value < 0 {
			memoryFindings = append(memoryFindings, finding(path+"."+name, "PROTOCOL_SIZE_INVALID", diagnostics.Error, "Byte sizes must be nonnegative; zero and accepted binary unit strings are preserved."))
		}
	}
	if len(memoryFindings) > 0 {
		return memoryFindings
	}
	raw, _ := json.Marshal(fields)
	if sbjson.UnmarshalContext(registry.Context(context.Background()), raw, consumer) != nil {
		return []diagnostics.Finding{finding(path, "PROTOCOL_OPTIONS_INVALID", diagnostics.Error, "Protocol fields, size units or durations are rejected by their pinned consumer schema.")}
	}
	if kind == "hysteria" {
		projection, _ := PrepareUpgrade(kind, side, path, source)
		for _, item := range projection.Findings {
			if item.Severity == diagnostics.Error {
				return projection.Findings
			}
		}
	}
	return nil
}

func ConfigFindings(source []byte) []diagnostics.Finding {
	var root map[string]json.RawMessage
	if json.Unmarshal(source, &root) != nil {
		return nil
	}
	var result []diagnostics.Finding
	for _, section := range []string{"inbounds", "outbounds"} {
		var entities []json.RawMessage
		if json.Unmarshal(root[section], &entities) != nil {
			continue
		}
		for i, entity := range entities {
			var header struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(entity, &header)
			side := "inbound"
			if section == "outbounds" {
				side = "outbound"
			}
			result = append(result, CurrentFindings(header.Type, side, fmt.Sprintf("%s[%d]", section, i), entity)...)
		}
	}
	return result
}

type Projection struct {
	Candidate json.RawMessage
	Findings  []diagnostics.Finding
}

// PrepareUpgrade is a pure historical transformation. Current, explicitly new
// Hysteria2 profiles can retain the new default; this function is only for old
// durable profiles and the generated client owner.
func PrepareUpgrade(kind, side, path string, source json.RawMessage) (Projection, error) {
	result := Projection{Candidate: append(json.RawMessage(nil), source...)}
	if kind != "hysteria" && kind != "hysteria2" {
		return result, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(source, &fields) != nil || fields == nil {
		result.Findings = append(result.Findings, finding(path, "PROTOCOL_OPTIONS_INVALID", diagnostics.Error, "Protocol options must be an object."))
		return result, diagnostics.FirstError(result.Findings)
	}
	if kind == "hysteria" {
		pairs := [][2]string{{"recv_window_conn", "connection_receive_window"}, {"disable_mtu_discovery", "disable_path_mtu_discovery"}}
		if side == "inbound" {
			pairs = append(pairs, [2]string{"recv_window_client", "stream_receive_window"}, [2]string{"max_conn_client", "max_concurrent_streams"})
		} else if side == "outbound" {
			pairs = append(pairs, [2]string{"recv_window", "stream_receive_window"})
		}
		for _, pair := range pairs {
			legacy, oldPresent := fields[pair[0]]
			if !oldPresent {
				continue
			}
			current, newPresent := fields[pair[1]]
			if newPresent && !equalJSON(legacy, current) {
				result.Findings = append(result.Findings, finding(path+"."+pair[0], "TRANSPORT_ALIAS_CONFLICT", diagnostics.Error, "Old and current fields differ. Choose one representation before retrying."))
				continue
			}
			if !newPresent {
				fields[pair[1]] = legacy
			}
			delete(fields, pair[0])
			result.Findings = append(result.Findings, finding(path+"."+pair[0], "HYSTERIA_ALIAS_MIGRATED", diagnostics.Warn, "The protocol-specific alias was moved without changing its value, including zero or false."))
		}
	}
	if kind == "hysteria2" && side == "outbound" {
		if _, present := fields["disable_chrome_parrot"]; !present {
			fields["disable_chrome_parrot"] = json.RawMessage("true")
			result.Findings = append(result.Findings, finding(path+".disable_chrome_parrot", "HYSTERIA2_LEGACY_HANDSHAKE_PRESERVED", diagnostics.Warn, "The new Chrome handshake default is disabled for this old profile, preserving certificate algorithms and QUIC tuning."))
		}
	}
	if err := diagnostics.FirstError(result.Findings); err != nil {
		return result, err
	}
	candidate, err := json.Marshal(fields)
	if err != nil {
		return result, err
	}
	if len(result.Findings) > 0 {
		result.Candidate = candidate
	}
	return result, nil
}

func equalJSON(left, right []byte) bool {
	var a, b any
	decoder := json.NewDecoder(bytes.NewReader(left))
	decoder.UseNumber()
	if decoder.Decode(&a) != nil {
		return false
	}
	decoder = json.NewDecoder(bytes.NewReader(right))
	decoder.UseNumber()
	if decoder.Decode(&b) != nil {
		return false
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func finding(path, code, severity, message string) diagnostics.Finding {
	outcome := diagnostics.AutomaticDiagnostic
	if severity == diagnostics.Error {
		outcome = diagnostics.ManualRequired
	}
	return diagnostics.Finding{Kind: "transport", Path: path, Code: code, Severity: severity, Message: message, MigrationOutcome: outcome, AutomaticAvailable: severity != diagnostics.Error, OperatorActionRequired: severity == diagnostics.Error}
}

// PrepareConfigUpgrade delegates only the actual top-level entity protocols;
// it never recursively renames similarly named HTTP/gRPC/TUIC fields.
func PrepareConfigUpgrade(source []byte) (Projection, error) {
	result := Projection{Candidate: append([]byte(nil), source...)}
	var root map[string]json.RawMessage
	if json.Unmarshal(source, &root) != nil || root == nil {
		return result, fmt.Errorf("protocol candidate must be an object")
	}
	changed := false
	for _, section := range []string{"inbounds", "outbounds"} {
		raw, present := root[section]
		if !present {
			continue
		}
		var entities []json.RawMessage
		if json.Unmarshal(raw, &entities) != nil {
			return result, fmt.Errorf("protocol section must be an array")
		}
		for i, entity := range entities {
			var header struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(entity, &header) != nil {
				return result, fmt.Errorf("protocol entity must be an object")
			}
			side := "inbound"
			if section == "outbounds" {
				side = "outbound"
			}
			projected, err := PrepareUpgrade(header.Type, side, fmt.Sprintf("%s[%d]", section, i), entity)
			result.Findings = append(result.Findings, projected.Findings...)
			if err != nil {
				continue
			}
			if !bytes.Equal(projected.Candidate, entity) {
				entities[i] = projected.Candidate
				changed = true
			}
		}
		root[section], _ = json.Marshal(entities)
	}
	if err := diagnostics.FirstError(result.Findings); err != nil {
		return result, err
	}
	if changed {
		result.Candidate, _ = json.Marshal(root)
	}
	return result, nil
}
