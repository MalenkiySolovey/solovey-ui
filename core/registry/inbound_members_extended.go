//go:build shtorm_extended

package registry

import (
	"errors"
	"slices"

	"github.com/sagernet/sing-box/option"
)

// InboundMembers interprets the actual decoded native schema. Persistence and
// the member tag references remain with the existing Inbounds owner.
func InboundMembers(kind string, value any) ([]option.Inbound, error) {
	var members []option.Inbound
	switch kind {
	case "bond":
		if group, ok := value.(*option.BondInboundOptions); ok {
			members = group.Inbounds
		}
	case "failover":
		if group, ok := value.(*option.FailoverInboundOptions); ok {
			members = group.Inbounds
		}
	default:
		return nil, nil
	}
	if len(members) == 0 || len(members) > 255 {
		return nil, errors.New("inbound group options invalid")
	}
	return slices.Clone(members), nil
}
