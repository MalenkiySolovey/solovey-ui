// Package types owns the panel's semantic identities, independently of storage,
// compiled runtime implementations and consumers such as Doctor or imports.
package types

type Mapping struct{ Category, Type, RuntimeType string }

const Warp = "warp"
const Failover = "failover"

var mappings = [...]Mapping{
	{Category: "endpoints", Type: Warp, RuntimeType: "wireguard"},
	{Category: "outbounds", Type: Failover, RuntimeType: "selector"},
}

func Mappings() []Mapping { return append([]Mapping(nil), mappings[:]...) }

// RuntimeType preserves unknown names; classification never invents an alias.
func RuntimeType(category, panelType string) string {
	for _, mapping := range mappings {
		if mapping.Category == category && mapping.Type == panelType {
			return mapping.RuntimeType
		}
	}
	return panelType
}
