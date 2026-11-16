// Package clientfacts combines portable protocol schema with current runtime admission.
package clientfacts

import (
	shared "github.com/MalenkiySolovey/solovey-host/subscriptions/clientfacts"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"sort"
)

type Fact = shared.Fact

func Facts() []Fact                         { return shared.Facts() }
func Resolve(panelType string) (Fact, bool) { return shared.Resolve(panelType) }
func SupportsUDP(panelType string, outbound map[string]any) bool {
	return shared.SupportsUDP(panelType, outbound)
}
func DeliveryOnly(panelType string) bool        { return shared.DeliveryOnly(panelType) }
func UserField(panelType string) (string, bool) { return shared.UserField(panelType) }
func UserFieldForMethod(panelType, method string) (string, bool) {
	return shared.UserFieldForMethod(panelType, method)
}

func CanDeliver(panelType, format string) bool {
	fact, ok := Resolve(panelType)
	if !ok || !entitycapabilities.Resolve("inbounds", panelType).Available {
		return false
	}
	switch format {
	case "json":
		return fact.JSON
	case "uri":
		return fact.URI
	default:
		return false
	}
}

func URITypes(availableOnly bool) []string {
	result := []string{}
	for _, fact := range shared.Facts() {
		if fact.URI && (!availableOnly || CanDeliver(fact.Type, "uri")) {
			result = append(result, fact.Type)
		}
	}
	sort.Strings(result)
	return result
}
