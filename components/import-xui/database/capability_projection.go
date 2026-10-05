//go:build !minimal

package importxui

import (
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/components/import-xui/database/mapping"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entitycapabilities "github.com/MalenkiySolovey/solovey-ui/internal/entities/capabilities"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/saveeligibility"
)

// Target facts are deployment-injected trusted data, never submitted plan data.
func targetCapabilities(injected *entitycapabilities.Snapshot) entitycapabilities.Snapshot {
	if injected == nil {
		return entitycapabilities.Current()
	}
	copy := *injected
	copy.Facts = append([]entitycapabilities.Fact(nil), injected.Facts...)
	return copy
}

type UnsupportedObject struct {
	Kind       string                  `json:"kind"`
	SrcID      any                     `json:"srcId"`
	SrcTag     string                  `json:"srcTag"`
	Type       string                  `json:"type"`
	Reason     string                  `json:"reason"`
	Capability entitycapabilities.Fact `json:"capability"`
}

func unsupportedCapabilityItem(kind string, id any, tag, panelType, reason string, fact entitycapabilities.Fact) PlanItem {
	item := warningOnlyItem(kind, id, tag, tag, []string{fmt.Sprintf("%s %q: type %q unavailable for import (%s)", kind, tag, panelType, reason)})
	item.Unsupported = []UnsupportedObject{{Kind: kind, SrcID: id, SrcTag: tag, Type: panelType, Reason: reason, Capability: fact}}
	return item
}

func sourceInboundCapability(snapshot entitycapabilities.Snapshot, protocol string) (string, entitycapabilities.Fact, string) {
	category := "inbounds"
	panelType := mapping.InboundType(protocol)
	if protocol == "wireguard" {
		category = "endpoints"
		panelType = protocol
	}
	if panelType == "" {
		panelType = protocol
	}
	fact := snapshot.Resolve(category, panelType)
	if !fact.Available {
		return category, fact, fact.Reason
	}
	if category == "inbounds" && mapping.InboundType(protocol) == "" {
		return category, fact, "IMPORT_MAPPING_UNSUPPORTED"
	}
	return category, fact, ""
}

func unsupportedObjects(plan MigrationPlan) []UnsupportedObject {
	result := []UnsupportedObject{}
	for _, item := range plan.Items {
		result = append(result, item.Unsupported...)
	}
	return result
}

func routingCapabilityIssues(snapshot entitycapabilities.Snapshot, endpoints []model.Endpoint, outbounds []model.Outbound) []UnsupportedObject {
	issues := []UnsupportedObject{}
	for _, endpoint := range endpoints {
		fact := snapshot.Resolve("endpoints", endpoint.Type)
		if !fact.Available {
			issues = append(issues, UnsupportedObject{Kind: KindEndpoint, SrcID: endpoint.Tag, SrcTag: endpoint.Tag, Type: endpoint.Type, Reason: fact.Reason, Capability: fact})
		}
	}
	for _, outbound := range outbounds {
		fact := snapshot.Resolve("outbounds", outbound.Type)
		if !fact.Available {
			issues = append(issues, UnsupportedObject{Kind: "outbound", SrcID: outbound.Tag, SrcTag: outbound.Tag, Type: outbound.Type, Reason: fact.Reason, Capability: fact})
		}
	}
	return issues
}

// Destination adapters require the actual current binary, even when a trusted
// preview target was injected. No target snapshot can authorize unavailable
// current runtime implementation.
func checkImportedCapability(category, panelType string) error {
	return saveeligibility.Check(category, panelType, "new", "")
}
