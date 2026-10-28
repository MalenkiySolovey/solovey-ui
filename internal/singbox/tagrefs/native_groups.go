package tagrefs

import (
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	outboundcomposition "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds/composition"
)

// ValidateNativeOutboundGroups checks references reachable from native groups
// using the same interpretation which protects rename/delete and eager reload.
// Candidate strings are filtered through the existing reference scanner so
// semantic policy values cannot accidentally become dependency edges.
func ValidateNativeOutboundGroups(rows []model.Outbound) error {
	byTag := make(map[string]model.Outbound, len(rows))
	for _, row := range rows {
		byTag[row.Tag] = row
	}
	state := make(map[string]uint8)
	var visit func(string, int) error
	visit = func(tag string, depth int) error {
		if state[tag] == 1 {
			return fmt.Errorf("OUTBOUND_GROUP_CYCLE: %q", tag)
		}
		if state[tag] == 2 {
			return nil
		}
		if depth > 64 {
			return fmt.Errorf("OUTBOUND_GROUP_DEPTH_EXCEEDED")
		}
		state[tag] = 1
		row := byTag[tag]
		opts := optionsMapOf(row.Options)
		candidates := make(map[string]bool)
		for _, field := range []string{"detour", "default", "final"} {
			if value, ok := opts[field].(string); ok {
				candidates[value] = true
			}
		}
		if values, ok := opts["outbounds"].([]any); ok {
			for _, value := range values {
				if tag, ok := value.(string); ok {
					candidates[tag] = true
				}
			}
		}
		members, err := outboundcomposition.Members(row.Type, row.Options)
		if err != nil {
			return err
		}
		for _, member := range members {
			candidates[member.Outbound] = true
		}
		for target := range candidates {
			if _, exists := byTag[target]; !exists || len(scanOutboundRowsForTag([]model.Outbound{row}, target, 0)) == 0 {
				continue
			}
			if err := visit(target, depth+1); err != nil {
				return err
			}
		}
		state[tag] = 2
		return nil
	}
	for _, row := range rows {
		if outboundcomposition.IsGroup(row.Type) {
			if err := visit(row.Tag, 0); err != nil {
				return err
			}
		}
	}
	return nil
}
