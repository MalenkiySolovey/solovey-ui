package singboxconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/rulepolicy"
	"github.com/sagernet/sing-box/option"
)

// RuleSetTags preserves the official scalar-or-list identity. Multiple tags
// represent templated instances; callers must not split the desired record.
func RuleSetTags(raw json.RawMessage) ([]string, error) {
	var scalar string
	if json.Unmarshal(raw, &scalar) == nil {
		if scalar == "" {
			return nil, fmt.Errorf("empty rule-set tag")
		}
		return []string{scalar}, nil
	}
	var tags []string
	if json.Unmarshal(raw, &tags) != nil || len(tags) == 0 {
		return nil, fmt.Errorf("rule-set tags must be a nonempty string or list")
	}
	for _, tag := range tags {
		if tag == "" {
			return nil, fmt.Errorf("empty rule-set tag")
		}
	}
	return tags, nil
}

func RuleSetFindings(config []byte, historical bool) []diagnostics.Finding {
	var root dnsObject
	if json.Unmarshal(config, &root) != nil {
		return []diagnostics.Finding{ruleSetFailure("config", "invalid_json", "Configuration must be an object.")}
	}
	var route dnsObject
	_ = json.Unmarshal(root["route"], &route)
	var sets []dnsObject
	if raw, present := route["rule_set"]; present && (json.Unmarshal(raw, &sets) != nil || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("["))) {
		return []diagnostics.Finding{ruleSetFailure("route.rule_set", "ruleset_collection_invalid", "Rule sets must be an array of objects.")}
	}
	var findings []diagnostics.Finding
	flat := map[string]bool{}
	seen := map[string]bool{}
	total := 0
	for i, set := range sets {
		path := fmt.Sprintf("route.rule_set[%d]", i)
		if set == nil {
			findings = append(findings, ruleSetFailure(path, "ruleset_schema_invalid", "Rule sets must be objects."))
			continue
		}
		tags, err := RuleSetTags(set["tag"])
		if err != nil {
			findings = append(findings, ruleSetFailure(path+".tag", "ruleset_tag_invalid", "Use a nonempty tag or tag list."))
			continue
		}
		for _, tag := range tags {
			if seen[tag] {
				findings = append(findings, ruleSetFailure(path+".tag", "ruleset_tag_duplicate", "All rule-set tags, including list members, must be unique."))
			}
			seen[tag] = true
		}
		if finding := managedRuSmartIdentityFinding(set, tags, path); finding != nil {
			findings = append(findings, *finding)
			continue
		}
		if raw, present := set["rules"]; present {
			if finding := headlessRuleBudget(raw, path+".rules", &total); finding != nil {
				findings = append(findings, *finding)
				continue
			}
		}
		var parsed option.RuleSet
		if json.Unmarshal(dnsJSON(set), &parsed) != nil {
			findings = append(findings, ruleSetFailure(path, "ruleset_schema_invalid", "Correct the rule-set type, fields and required {tag} placeholders using the pinned schema."))
			continue
		}
		if len(tags) > 1 {
			for _, tag := range tags {
				if strings.ContainsAny(tag, "/\\") || tag == "." || tag == ".." {
					findings = append(findings, ruleSetFailure(path+".tag", "ruleset_tag_path_invalid", "Templated tags must not introduce path traversal."))
					break
				}
			}
		}
		if dnsString(set, "type") == "remote" {
			parsedURL, err := url.Parse(strings.ReplaceAll(dnsString(set, "url"), "{tag}", "identity"))
			if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
				findings = append(findings, ruleSetFailure(path+".url", "ruleset_url_invalid", "Remote rule sets require an HTTP or HTTPS URL."))
			}
		}
		if initial := dnsString(set, "initial_path"); initial != "" && initial != managedRuSmartRuleSetRelativePath && initial != managedRuSmartRuleSetPath() {
			findings = append(findings, ruleSetFailure(path+".initial_path", "ruleset_initial_path_unmanaged", "Select the existing owner-managed rule-set seed path; arbitrary host paths are unavailable."))
		}
		var rules []dnsObject
		_ = json.Unmarshal(set["rules"], &rules)
		isFlat := len(rules) == 1 && dnsString(rules[0], "type") != "logical" && !dnsBool(rules[0], "invert")
		if len(tags) == 1 && tags[0] == managedRuSmartRuleSetTag && dnsString(set, "type") == "local" && (dnsString(set, "path") == managedRuSmartRuleSetRelativePath || dnsString(set, "path") == managedRuSmartRuleSetPath()) {
			isFlat = true
		}
		for _, tag := range tags {
			flat[tag] = isFlat
		}
	}
	// The same bounded traversal covers nested route and DNS references. W1
	// still owns action validation. This owner only reasons about set identity
	// and the changed grouped matching boundary.
	for _, section := range []string{"route", "dns"} {
		var object dnsObject
		_ = json.Unmarshal(root[section], &object)
		var rules []json.RawMessage
		_ = json.Unmarshal(object["rules"], &rules)
		type node struct {
			raw   json.RawMessage
			path  string
			depth int
		}
		queue := make([]node, 0, len(rules))
		for i, raw := range rules {
			queue = append(queue, node{raw, fmt.Sprintf("%s.rules[%d]", section, i), 1})
		}
		for cursor := 0; cursor < len(queue); cursor++ {
			current := queue[cursor]
			if cursor >= rulepolicy.MaxNodes || current.depth > rulepolicy.MaxDepth {
				findings = append(findings, ruleSetFailure(current.path, "rule_budget_exceeded", "Reduce the rule tree to the supported depth and node budget."))
				break
			}
			var rule dnsObject
			if json.Unmarshal(current.raw, &rule) != nil {
				continue
			}
			if raw, present := rule["rule_set"]; present {
				tags, err := RuleSetTags(raw)
				if err != nil {
					findings = append(findings, ruleSetFailure(current.path+".rule_set", "ruleset_reference_invalid", "Select existing rule-set tags."))
				} else {
					for _, tag := range tags {
						if !seen[tag] {
							findings = append(findings, ruleSetFailure(current.path+".rule_set", "ruleset_reference_missing", "The referenced rule set is missing; restore or correct its definition."))
							continue
						}
						if historical && !flat[tag] && groupedRulePredicates(rule) {
							findings = append(findings, ruleSetFailure(current.path+".rule_set", "ruleset_group_semantics_manual", "Review grouped predicates against the custom multi/logical/inverted or unverified rule set under 1.14 semantics. Preserve the original until that decision is explicit."))
						}
					}
				}
			}
			if dnsString(rule, "type") == "logical" {
				var children []json.RawMessage
				_ = json.Unmarshal(rule["rules"], &children)
				if len(queue)+len(children) > rulepolicy.MaxNodes {
					findings = append(findings, ruleSetFailure(current.path, "rule_budget_exceeded", "Reduce the rule tree to the supported node budget."))
					break
				}
				for i, raw := range children {
					queue = append(queue, node{raw, fmt.Sprintf("%s.rules[%d]", current.path, i), current.depth + 1})
				}
			}
		}
	}
	return findings
}

func groupedRulePredicates(rule dnsObject) bool {
	for _, key := range []string{"domain", "domain_suffix", "domain_keyword", "domain_regex", "source_ip_cidr", "source_ip_is_private", "ip_cidr", "ip_is_private", "source_port", "source_port_range", "port", "port_range"} {
		if activeDNSRaw(rule[key]) {
			return true
		}
	}
	return false
}

func headlessRuleBudget(raw json.RawMessage, path string, total *int) *diagnostics.Finding {
	type node struct {
		raw   json.RawMessage
		path  string
		depth int
	}
	var rules []json.RawMessage
	if json.Unmarshal(raw, &rules) != nil || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		f := ruleSetFailure(path, "ruleset_rules_invalid", "Headless rules must be an array.")
		return &f
	}
	queue := make([]node, 0, len(rules))
	for i, raw := range rules {
		queue = append(queue, node{raw, fmt.Sprintf("%s[%d]", path, i), 1})
	}
	for cursor := 0; cursor < len(queue); cursor++ {
		current := queue[cursor]
		*total++
		if *total > rulepolicy.MaxNodes || current.depth > rulepolicy.MaxDepth {
			f := ruleSetFailure(current.path, "rule_budget_exceeded", "Reduce the headless rule tree to the shared depth and node budget.")
			return &f
		}
		var rule dnsObject
		if json.Unmarshal(current.raw, &rule) != nil || rule == nil {
			f := ruleSetFailure(current.path, "ruleset_rules_invalid", "Headless rules must be objects.")
			return &f
		}
		if dnsString(rule, "type") == "logical" {
			var children []json.RawMessage
			if json.Unmarshal(rule["rules"], &children) != nil {
				f := ruleSetFailure(current.path, "ruleset_rules_invalid", "Logical headless rules require an array of children.")
				return &f
			}
			if len(queue)+len(children) > rulepolicy.MaxNodes {
				f := ruleSetFailure(current.path, "rule_budget_exceeded", "Reduce the headless rule tree to the shared node budget.")
				return &f
			}
			for i, raw := range children {
				queue = append(queue, node{raw, fmt.Sprintf("%s.rules[%d]", current.path, i), current.depth + 1})
			}
		}
	}
	return nil
}
func ruleSetFailure(path, code, message string) diagnostics.Finding {
	finding := httpFailure(path, code, message)
	finding.Kind = "rule_set"
	return finding
}
