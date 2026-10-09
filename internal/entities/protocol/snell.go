package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/core/registry"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/sagernet/sing-box/option"
	sbjson "github.com/sagernet/sing/common/json"
)

func snellFindings(side, path string, source json.RawMessage) []diagnostics.Finding {
	bad := func(field, code, message string) []diagnostics.Finding {
		return []diagnostics.Finding{finding(path+field, code, diagnostics.Error, message)}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(source, &fields) != nil || fields == nil {
		return bad("", "SNELL_OPTIONS_INVALID", "Snell options must be an object.")
	}
	var users []option.SnellUser
	if raw, ok := fields["users"]; ok {
		if side != "inbound" {
			return bad(".users", "SNELL_OPTIONS_INVALID", "Snell outbound requires a single client user key.")
		}
		if json.Unmarshal(raw, &users) != nil {
			return bad(".users", "SNELL_USERS_INVALID", "Snell users must contain names and user keys.")
		}
	}
	delete(fields, "users")
	delete(fields, "type")
	delete(fields, "tag")
	raw, _ := json.Marshal(fields)
	var consumer any = &option.SnellOutboundOptions{}
	category := "outbounds"
	if side == "inbound" {
		consumer = &option.SnellInboundOptions{}
		category = "inbounds"
	}
	if sbjson.UnmarshalContext(registry.Context(context.Background()), raw, consumer) != nil {
		return bad("", "SNELL_OPTIONS_INVALID", "Snell fields are rejected by the selected version's pinned schema.")
	}
	contract := registry.SnellContract(category)
	var version int
	var psk, mode, obfs, userKey string
	if options, ok := consumer.(*option.SnellInboundOptions); ok {
		version, psk, mode, obfs = options.Version, options.PSK, options.V6Options.Mode, options.ObfsOptions.ObfsMode
	} else {
		options := consumer.(*option.SnellOutboundOptions)
		version, psk, mode, obfs, userKey = options.Version, options.PSK, options.V6Options.Mode, options.ObfsOptions.ObfsMode, options.UserKey
	}
	for _, fact := range contract.Versions {
		if version != fact.Version {
			continue
		}
		if len(psk) < fact.PSKMinBytes || fact.PSKMaxBytes > 0 && len(psk) > fact.PSKMaxBytes {
			return bad(".psk", "SNELL_PSK_LENGTH_INVALID", "PSK byte length is incompatible with the selected Snell version.")
		}
		if fact.Obfuscation && obfs != "" && !slices.Contains(contract.ObfsModes[:], strings.ToLower(obfs)) {
			return bad(".obfs_mode", "SNELL_OBFS_INVALID", "Obfuscation is unsupported by this Snell version.")
		}
		if !fact.Obfuscation && mode != "" && !slices.Contains(contract.Modes[:], mode) {
			return bad(".mode", "SNELL_MODE_INVALID", "Traffic-shaping mode is unsupported by this Snell version.")
		}
	}
	if len(userKey) > contract.UserKeyMaxBytes {
		return bad(".userkey", "SNELL_USERKEY_INVALID", "Snell user keys cannot exceed the protocol byte limit.")
	}
	seenKeys, seenNames := map[string]bool{}, map[string]bool{}
	for index, user := range users {
		if len(user.UserKey) == 0 || len(user.UserKey) > contract.UserKeyMaxBytes || user.Name == "" || seenKeys[user.UserKey] || seenNames[user.Name] {
			return bad(fmt.Sprintf(".users[%d]", index), "SNELL_USERS_INVALID", "Managed Snell users require unique nonempty names and valid unique user keys.")
		}
		seenKeys[user.UserKey], seenNames[user.Name] = true, true
	}
	return nil
}
