package outbounds

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

// AssembleFailoverForCore turns panel-only failover metadata into the selector
// schema accepted by sing-box.
func AssembleFailoverForCore(outbound model.Outbound, directTag string) (json.RawMessage, error) {
	opts, err := parseFailoverOptions(outbound.Options)
	if err != nil {
		return nil, err
	}
	if len(opts.Outbounds) == 0 {
		return nil, common.NewErrorf("failover group %q has no members", outbound.Tag)
	}
	members := append([]string(nil), opts.Outbounds...)
	if directTag != "" && !contains(members, directTag) {
		members = append(members, directTag)
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
	return json.Marshal(selector)
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}
