package tagrefs

import (
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func transportDownloadDetour(options map[string]any) string {
	transport, _ := options["transport"].(map[string]any)
	if transport["type"] != "xhttp" {
		return ""
	}
	download, _ := transport["download"].(map[string]any)
	detour, _ := download["detour"].(string)
	return detour
}

// Split download legs borrow the existing outbound/endpoint namespace. The
// reference scanner remains the authority for edges, including group policy.
func ValidateTransportDownloadReferences(rows []model.Outbound, endpoints []model.Endpoint) error {
	byTag := make(map[string]model.Outbound, len(rows)+len(endpoints))
	endpointTags := make(map[string]bool, len(endpoints))
	var roots []string
	for _, row := range rows {
		byTag[row.Tag] = row
		if transportDownloadDetour(optionsMapOf(row.Options)) != "" {
			roots = append(roots, row.Tag)
		}
	}
	if len(roots) == 0 {
		return nil
	}
	for _, row := range endpoints {
		byTag[row.Tag] = model.Outbound{Type: row.Type, Tag: row.Tag, Options: row.Options}
		endpointTags[row.Tag] = true
	}
	for _, root := range roots {
		target := transportDownloadDetour(optionsMapOf(byTag[root].Options))
		if _, found := byTag[target]; !found {
			return fmt.Errorf("TRANSPORT_DETOUR_MISSING: select an existing outbound or endpoint")
		}
	}
	state := make(map[string]uint8)
	var visit func(string, int) error
	visit = func(tag string, depth int) error {
		if state[tag] == 1 {
			return fmt.Errorf("TRANSPORT_DETOUR_CYCLE: outbound dependencies form a cycle")
		}
		if state[tag] == 2 {
			return nil
		}
		if depth > 64 {
			return fmt.Errorf("TRANSPORT_DETOUR_DEPTH_EXCEEDED")
		}
		state[tag] = 1
		row := byTag[tag]
		candidates := map[string]bool{}
		queue := []any{optionsMapOf(row.Options)}
		for len(queue) != 0 {
			node := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			switch node := node.(type) {
			case string:
				if _, found := byTag[node]; found {
					candidates[node] = true
				}
			case map[string]any:
				for _, value := range node {
					queue = append(queue, value)
				}
			case []any:
				queue = append(queue, node...)
			}
		}
		for target := range candidates {
			var refs []TagReference
			if endpointTags[tag] {
				refs = scanEndpointRowsForTag([]model.Endpoint{{Type: row.Type, Tag: tag, Options: row.Options}}, target, 0)
			} else {
				refs = scanOutboundRowsForTag([]model.Outbound{row}, target, 0)
			}
			if len(refs) != 0 {
				if err := visit(target, depth+1); err != nil {
					return err
				}
			}
		}
		state[tag] = 2
		return nil
	}
	for _, root := range roots {
		if err := visit(root, 0); err != nil {
			return err
		}
	}
	return nil
}
