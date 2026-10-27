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
	if kind != "bond" {
		return nil, nil
	}
	group, ok := value.(*option.BondInboundOptions)
	if !ok || len(group.Inbounds) == 0 || len(group.Inbounds) > 255 {
		return nil, errors.New("inbound group options invalid")
	}
	return slices.Clone(group.Inbounds), nil
}
