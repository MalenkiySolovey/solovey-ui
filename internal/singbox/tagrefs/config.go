package tagrefs

import (
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func scanConfigBlobForOutboundTag(blob []byte, tag string) ([]TagReference, error) {
	if len(blob) == 0 {
		return nil, nil
	}
	var config struct {
		Dns struct {
			Servers []map[string]any `json:"servers"`
		} `json:"dns"`
		Ntp struct {
			Detour string `json:"detour"`
		} `json:"ntp"`
		Route struct {
			Final   string           `json:"final"`
			Rules   []map[string]any `json:"rules"`
			RuleSet []map[string]any `json:"rule_set"`
		} `json:"route"`
		Experimental struct {
			ClashAPI struct {
				ExternalUIDownloadDetour string `json:"external_ui_download_detour"`
			} `json:"clash_api"`
		} `json:"experimental"`
	}
	if err := json.Unmarshal(blob, &config); err != nil {
		return nil, common.NewError("config blob is malformed, cannot resolve tag references: ", err.Error())
	}

	var refs []TagReference
	for i, server := range config.Dns.Servers {
		if detour, _ := server["detour"].(string); detour == tag {
			refs = append(refs, TagReference{
				Kind:    "dns server",
				Locator: fmt.Sprintf("dns server %s (detour)", nameOrIndex(server["tag"], i)),
			})
		}
	}
	if config.Ntp.Detour == tag {
		refs = append(refs, TagReference{Kind: "ntp", Locator: "ntp (detour)"})
	}
	for i, rule := range config.Route.Rules {
		refs = appendRouteRuleRefs(refs, rule, i, tag)
	}
	for i, ruleSet := range config.Route.RuleSet {
		if detour, _ := ruleSet["download_detour"].(string); detour == tag {
			refs = append(refs, TagReference{
				Kind:    "rule_set",
				Locator: fmt.Sprintf("rule_set %s (download_detour)", nameOrIndex(ruleSet["tag"], i)),
			})
		}
	}
	if config.Route.Final == tag {
		refs = append(refs, TagReference{Kind: "route final", Locator: "route final", Lazy: true})
	}
	if config.Experimental.ClashAPI.ExternalUIDownloadDetour == tag {
		refs = append(refs, TagReference{Kind: "clash_api", Locator: "clash_api (external_ui_download_detour)"})
	}
	return refs, nil
}

func appendRouteRuleRefs(refs []TagReference, rule map[string]any, index int, tag string) []TagReference {
	if outbound, _ := rule["outbound"].(string); outbound == tag {
		refs = append(refs, TagReference{
			Kind:    "route rule",
			Locator: fmt.Sprintf("route rule #%d (outbound)", index),
			Lazy:    true,
		})
	}
	nested, _ := rule["rules"].([]any)
	for _, item := range nested {
		if sub, ok := item.(map[string]any); ok {
			refs = appendRouteRuleRefs(refs, sub, index, tag)
		}
	}
	return refs
}

func nameOrIndex(name any, index int) string {
	if s, ok := name.(string); ok && s != "" {
		return fmt.Sprintf("%q", s)
	}
	return fmt.Sprintf("#%d", index)
}
