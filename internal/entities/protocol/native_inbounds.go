package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/composition"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	entitytypes "github.com/MalenkiySolovey/solovey-ui/internal/entities/types"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	sbjson "github.com/sagernet/sing/common/json"
)

// The selected registry supplies the actual native schema. No Shtorm option
// types or implementation imports cross into the common entity owner.
func nativeInboundFindings(kind, side, path string, source json.RawMessage) []diagnostics.Finding {
	return nativeInboundOptionsFindings(kind, side, path, source, false)
}

func nativeInboundOptionsFindings(kind, side, path string, source json.RawMessage, resolvedGroup bool) []diagnostics.Finding {
	runtimeKind := entitytypes.RuntimeType("inbounds", kind)
	if side != "inbound" || !registry.Resolve("inbounds", runtimeKind).Available() {
		// Save eligibility and runtime projection preserve unavailable owner
		// state. A different provider's schema must not normalize its fields.
		return nil
	}
	bad := func(field, code, message string) []diagnostics.Finding {
		return []diagnostics.Finding{finding(path+field, code, diagnostics.Error, message)}
	}
	if jsonvalue.StrictObject(source) != nil {
		return bad("", "NATIVE_OPTIONS_INVALID", "Native protocol options must be an unambiguous object.")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(source, &fields) != nil {
		return bad("", "NATIVE_OPTIONS_INVALID", "Native protocol options must be an object.")
	}
	delete(fields, "type")
	delete(fields, "tag")
	if kind == "sudoku" {
		delete(fields, "master_key")
	}
	if composition.IsGroup(kind) && !resolvedGroup {
		if _, err := composition.Members(kind, source); err != nil {
			return bad(".inbounds", "INBOUND_GROUP_MEMBERS_INVALID", "Select canonical inbound tags for the native group.")
		}
		// Storage uses canonical tag references. The owner resolves them into
		// native option objects only in the final serving projection.
		delete(fields, "inbounds")
	}
	if kind == "ssh" {
		// The Inbounds owner validates and seals explicit private-key mutations.
		// The complete resolved candidate is checked by the actual provider.
		delete(fields, "host_key")
		var version string
		if raw, present := fields["server_version"]; present && (json.Unmarshal(raw, &version) != nil || !validSSHServerVersion(version)) {
			return bad(".server_version", "SSH_SERVER_VERSION_INVALID", "SSH server version must be a bounded SSH-2.0 identification string.")
		}
	}
	consumer, registered := registry.InboundRegistry().CreateOptions(runtimeKind)
	raw, _ := json.Marshal(fields)
	if !registered || sbjson.UnmarshalContextDisallowUnknownFields(registry.Context(context.Background()), raw, consumer) != nil {
		return bad("", "NATIVE_OPTIONS_INVALID", "Native protocol fields are rejected by the pinned serving schema.")
	}
	if composition.IsGroup(kind) && resolvedGroup {
		members, err := registry.InboundMembers(runtimeKind, consumer)
		if err != nil {
			return bad(".inbounds", "INBOUND_GROUP_MEMBERS_INVALID", "The resolved native group must contain its owned members.")
		}
		var result []diagnostics.Finding
		for i, member := range members {
			if composition.IsGroup(entitytypes.CanonicalType("inbounds", member.Type)) {
				return bad(".inbounds", "INBOUND_GROUP_NESTING_INVALID", "Native groups cannot contain another group.")
			}
			child, err := member.MarshalJSONContext(registry.Context(context.Background()))
			if err != nil {
				return bad(".inbounds", "NATIVE_OPTIONS_INVALID", "A resolved group member is invalid.")
			}
			result = append(result, CurrentFindings(member.Type, side, fmt.Sprintf("%s.inbounds[%d]", path, i), child)...)
		}
		return result
	}
	if kind == "mieru" {
		var transport string
		if raw, present := fields["transport"]; present && (json.Unmarshal(raw, &transport) != nil || !slices.Contains([]string{"TCP", "UDP"}, transport)) {
			return bad(".transport", "MIERU_TRANSPORT_INVALID", "Mieru transport must be TCP or UDP.")
		}
	}
	if kind == "trusttunnel" {
		var congestion string
		if raw, present := fields["congestion_controller"]; present && (json.Unmarshal(raw, &congestion) != nil || !slices.Contains([]string{"", "bbr", "cubic", "reno"}, congestion)) {
			return bad(".congestion_controller", "TRUSTTUNNEL_CONGESTION_UNSUPPORTED", "The pinned TrustTunnel consumer supports bbr, cubic and reno.")
		}
	}
	return nil
}

// x/crypto/ssh exchanges an ASCII SSH-2.0 identification line, including CRLF
// within its 255-byte bound. Reject values which could fail only at handshake.
func validSSHServerVersion(version string) bool {
	if version == "" {
		return true
	}
	if !strings.HasPrefix(version, "SSH-2.0-") || len(version) <= len("SSH-2.0-") || len(version) > 253 || version[len("SSH-2.0-")] == ' ' {
		return false
	}
	for _, character := range []byte(version) {
		if character < 32 || character > 126 {
			return false
		}
	}
	return true
}
