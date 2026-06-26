package tagrefs

import (
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func ssmServersOf(row model.Service) map[string]string {
	if row.Type != "ssm-api" || len(row.Options) == 0 {
		return nil
	}
	var opts struct {
		Servers map[string]string `json:"servers"`
	}
	if err := json.Unmarshal(row.Options, &opts); err != nil {
		return nil
	}
	return opts.Servers
}

func scanServiceRowsForInboundTag(rows []model.Service, tag string) []TagReference {
	var refs []TagReference
	for _, row := range rows {
		for path, inboundTag := range ssmServersOf(row) {
			if inboundTag == tag {
				refs = append(refs, TagReference{
					Kind:    "ssm-api service",
					Locator: fmt.Sprintf("ssm-api service %q (servers[%q])", row.Tag, path),
				})
			}
		}
	}
	return refs
}

func ssmServiceIdsReferencingInbound(rows []model.Service, tag string) []uint {
	var ids []uint
	for _, row := range rows {
		for _, inboundTag := range ssmServersOf(row) {
			if inboundTag == tag {
				ids = append(ids, row.Id)
				break
			}
		}
	}
	return ids
}

func optionsMapOf(options json.RawMessage) map[string]any {
	if len(options) == 0 {
		return nil
	}
	var opts map[string]any
	if err := json.Unmarshal(options, &opts); err != nil {
		return nil
	}
	return opts
}

func scanOutboundRowsForTag(rows []model.Outbound, tag string, excludeID uint) []TagReference {
	var refs []TagReference
	for _, row := range rows {
		if row.Id == excludeID {
			continue
		}
		opts := optionsMapOf(row.Options)
		if opts == nil {
			continue
		}
		if detour, _ := opts["detour"].(string); detour == tag {
			refs = append(refs, TagReference{
				Kind:    "outbound detour",
				Locator: fmt.Sprintf("outbound %q (detour)", row.Tag),
			})
		}
		if row.Type != "selector" && row.Type != "urltest" && row.Type != "failover" {
			continue
		}
		if members, _ := opts["outbounds"].([]any); containsTag(members, tag) {
			refs = append(refs, TagReference{
				Kind:    "group member",
				Locator: fmt.Sprintf("%s %q (outbounds list)", row.Type, row.Tag),
			})
		}
		if def, _ := opts["default"].(string); def == tag {
			refs = append(refs, TagReference{
				Kind:    "group default",
				Locator: fmt.Sprintf("%s %q (default)", row.Type, row.Tag),
			})
		}
	}
	return refs
}

func containsTag(members []any, tag string) bool {
	for _, member := range members {
		if s, ok := member.(string); ok && s == tag {
			return true
		}
	}
	return false
}

func scanEndpointRowsForTag(rows []model.Endpoint, tag string, excludeID uint) []TagReference {
	var refs []TagReference
	for _, row := range rows {
		if row.Id == excludeID {
			continue
		}
		opts := optionsMapOf(row.Options)
		if detour, _ := opts["detour"].(string); detour == tag {
			refs = append(refs, TagReference{
				Kind:    "endpoint detour",
				Locator: fmt.Sprintf("endpoint %q (detour)", row.Tag),
			})
		}
	}
	return refs
}

func scanServiceRowsForOutboundDetour(rows []model.Service, tag string) []TagReference {
	var refs []TagReference
	for _, row := range rows {
		opts := optionsMapOf(row.Options)
		if detour, _ := opts["detour"].(string); detour == tag {
			refs = append(refs, TagReference{
				Kind:    "service detour",
				Locator: fmt.Sprintf("%s service %q (detour)", row.Type, row.Tag),
			})
		}
	}
	return refs
}
