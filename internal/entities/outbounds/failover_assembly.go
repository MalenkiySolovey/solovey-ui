package outbounds

import (
	"encoding/json"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func failoverRejectOutboundConfig() json.RawMessage {
	return json.RawMessage(`{"type":"block","tag":"` + FailoverRejectOutboundTag + `"}`)
}

func isFailoverRejectSupportConfig(config json.RawMessage) bool {
	var payload struct {
		Type string `json:"type"`
		Tag  string `json:"tag"`
	}
	if err := json.Unmarshal(config, &payload); err != nil {
		return false
	}
	return payload.Type == "block" && payload.Tag == FailoverRejectOutboundTag
}

// AssembleFailoverForCore turns panel-only failover metadata into the selector
// schema accepted by sing-box.
func AssembleFailoverForCore(outbound model.Outbound, directTag string) (json.RawMessage, error) {
	configs, err := AssembleFailoverOutboundsForCore(outbound, directTag)
	if err != nil {
		return nil, err
	}
	return configs[len(configs)-1], nil
}

// AssembleFailoverOutboundsForCore returns every runtime outbound required by
// the panel-level failover group. The last element is always the selector that
// represents the saved failover row; preceding elements are support outbounds
// that exist only in generated sing-box configs.
func AssembleFailoverOutboundsForCore(outbound model.Outbound, directTag string) ([]json.RawMessage, error) {
	opts, err := parseFailoverOptions(outbound.Options)
	if err != nil {
		return nil, err
	}
	if len(opts.Outbounds) == 0 {
		return nil, common.NewErrorf("failover group %q has no members", outbound.Tag)
	}
	members := append([]string(nil), opts.Outbounds...)
	finalTag := strings.TrimSpace(opts.finalTag(directTag))
	if finalTag != "" && !contains(members, finalTag) {
		members = append(members, finalTag)
	}
	selector := map[string]any{
		"type":      "selector",
		"tag":       outbound.Tag,
		"outbounds": members,
		"default":   opts.Outbounds[0],
	}
	if opts.InterruptExistConnections != nil {
		selector["interrupt_exist_connections"] = *opts.InterruptExistConnections
	}
	selectorJSON, err := json.Marshal(selector)
	if err != nil {
		return nil, err
	}
	if opts.finalChoice() == FailoverFinalReject {
		return []json.RawMessage{failoverRejectOutboundConfig(), selectorJSON}, nil
	}
	return []json.RawMessage{selectorJSON}, nil
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
