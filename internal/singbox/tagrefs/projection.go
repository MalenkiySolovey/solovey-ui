package tagrefs

import (
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

// ProjectionRows contains only objects selected by the entity owner. Omitted
// historical objects cannot create references that exist only in storage.
type ProjectionRows struct {
	Inbounds  []model.Inbound
	Outbounds []model.Outbound
	Endpoints []model.Endpoint
	Services  []model.Service
}

func ProjectedReferences(base []byte, rows ProjectionRows, category, tag string) ([]TagReference, error) {
	var refs []TagReference
	switch category {
	case "outbounds", "endpoints":
		refs = append(refs, scanOutboundRowsForTag(rows.Outbounds, tag, 0)...)
		refs = append(refs, scanEndpointRowsForTag(rows.Endpoints, tag, 0)...)
		refs = append(refs, scanServiceRowsForOutboundDetour(rows.Services, tag)...)
		baseRefs, err := scanConfigBlobForOutboundTag(base, tag)
		if err != nil {
			return nil, err
		}
		refs = append(refs, baseRefs...)
	case "inbounds":
		refs = append(refs, scanServiceRowsForInboundTag(rows.Services, tag)...)
		for _, row := range rows.Inbounds {
			if detour, _ := optionsMapOf(row.Options)["detour"].(string); detour == tag {
				refs = append(refs, TagReference{Kind: "inbound detour", Locator: fmt.Sprintf("inbound %q (detour)", row.Tag)})
			}
		}
	}
	extra, err := projectedBaseReferences(base, category, tag)
	if err != nil {
		return nil, err
	}
	refs = append(refs, extra...)
	if category == "endpoints" {
		refs = append(refs, endpointServiceReferences(rows.Services, tag)...)
		refs = forEndpoint(refs)
	}
	return refs, nil
}

func endpointServiceReferences(rows []model.Service, tag string) []TagReference {
	var refs []TagReference
	for _, row := range rows {
		if row.Type == "derp" && listHasTag(optionsMapOf(row.Options)["verify_client_endpoint"], tag) {
			refs = append(refs, TagReference{Kind: "service endpoint", Locator: fmt.Sprintf("service %q (verify_client_endpoint)", row.Tag)})
		}
	}
	return refs
}

func projectedBaseReferences(base []byte, category, tag string) ([]TagReference, error) {
	if len(base) == 0 {
		return nil, nil
	}
	var document map[string]any
	if err := json.Unmarshal(base, &document); err != nil {
		return nil, err
	}
	var refs []TagReference
	for _, section := range []string{"dns", "route"} {
		node, _ := document[section].(map[string]any)
		if category == "inbounds" {
			refs = appendInboundRuleReferences(refs, node["rules"], section+".rules", tag)
		}
		servers, _ := node["servers"].([]any)
		for index, value := range servers {
			server, _ := value.(map[string]any)
			field := ""
			if category == "endpoints" {
				field = "endpoint"
			} else if category == "services" {
				field = "service"
			}
			if field != "" && server[field] == tag {
				refs = append(refs, TagReference{Kind: "dns " + field, Locator: fmt.Sprintf("%s.servers[%d].%s", section, index, field)})
			}
		}
	}
	return refs, nil
}

func listHasTag(value any, tag string) bool {
	if scalar, ok := value.(string); ok {
		return scalar == tag
	}
	values, _ := value.([]any)
	return containsTag(values, tag)
}

func appendInboundRuleReferences(refs []TagReference, value any, path, tag string) []TagReference {
	// Iterative traversal keeps historical nested rules bounded by JSON input,
	// rather than consuming the goroutine stack before diagnostics can run.
	type node struct {
		value any
		path  string
	}
	queue := []node{{value, path}}
	for len(queue) > 0 {
		current := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		values, _ := current.value.([]any)
		for i, value := range values {
			rule, _ := value.(map[string]any)
			location := fmt.Sprintf("%s[%d]", current.path, i)
			if listHasTag(rule["inbound"], tag) {
				refs = append(refs, TagReference{Kind: "inbound rule", Locator: location + ".inbound", Lazy: true})
			}
			if nested, ok := rule["rules"].([]any); ok {
				queue = append(queue, node{nested, location + ".rules"})
			}
		}
	}
	return refs
}
