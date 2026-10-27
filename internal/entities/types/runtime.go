// Package types owns the panel's semantic identities, independently of storage,
// compiled runtime implementations and consumers such as Doctor or imports.
package types

type Mapping struct {
	Category, Type, RuntimeType string
	CanonicalOnly               bool
}

const Warp = "warp"
const Failover = "failover"
const CoreFailover = "core-failover"

var mappings = [...]Mapping{
	{Category: "endpoints", Type: Warp, RuntimeType: "wireguard"},
	{Category: "outbounds", Type: Failover, RuntimeType: "selector"},
	{Category: "inbounds", Type: CoreFailover, RuntimeType: "failover", CanonicalOnly: true},
}

func Mappings() []Mapping { return append([]Mapping(nil), mappings[:]...) }

// Runtime-only aliases are accepted only after the canonical owner resolves
// stored references. They cannot expose another public editor/save identity.
func IsRuntimeOnly(category, kind string) bool {
	for _, mapping := range mappings {
		if mapping.Category == category && mapping.RuntimeType == kind && mapping.CanonicalOnly {
			return true
		}
	}
	return false
}

func CanonicalType(category, runtimeType string) string {
	for _, mapping := range mappings {
		if mapping.Category == category && mapping.RuntimeType == runtimeType && mapping.CanonicalOnly {
			return mapping.Type
		}
	}
	return runtimeType
}

// RuntimeType preserves unknown names; classification never invents an alias.
func RuntimeType(category, panelType string) string {
	for _, mapping := range mappings {
		if mapping.Category == category && mapping.Type == panelType {
			return mapping.RuntimeType
		}
	}
	return panelType
}
