package validation

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"github.com/sagernet/sing-box/option"
)

const (
	RuleLosslessAutomatic   = "LOSSLESS_AUTOMATIC"
	RuleAutomaticDiagnostic = "AUTOMATIC_WITH_EXPLICIT_DIAGNOSTIC"
	RuleManualRequired      = "MANUAL_REQUIRED"
	RuleUnsupportedLegacy   = "UNSUPPORTED_LEGACY"
)

// RuleCompatibility is a read-only upgrade projection. Original is never
// rewritten; Candidate is publishable only after complete candidate validation
// and startup. No schema marker or second durable configuration is introduced.
type RuleCompatibility struct {
	Original  []byte        `json:"-"`
	Candidate []byte        `json:"-"`
	Outcome   string        `json:"outcome"`
	Findings  []RuleFinding `json:"findings"`
}

// Official 1.14 uses precisely these public action envelope types to reject
// nested keys. Reflection localizes a stable diagnostic; the official decoder
// remains the grammar authority for all conditions and action values.
var routeActionFields = actionFieldNames(reflect.TypeFor[option.RuleAction](), reflect.TypeFor[option.RouteActionOptions]())
var dnsActionFields = actionFieldNames(reflect.TypeFor[option.DNSRuleAction](), reflect.TypeFor[option.DNSRouteActionOptions](), reflect.TypeFor[option.DNSEvaluateActionOptions]())

func actionFieldNames(types ...reflect.Type) []string {
	fields := map[string]bool{}
	var collect func(reflect.Type)
	collect = func(t reflect.Type) {
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "-" {
				continue
			}
			if field.Anonymous && name == "" {
				collect(field.Type)
				continue
			}
			if name != "" {
				fields[name] = true
			}
		}
	}
	for _, t := range types {
		collect(t)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func nestedActionFindings(kind string, tree []rawRule, nested bool) []RuleFinding {
	var findings []RuleFinding
	keys := routeActionFields
	if kind == "dns" {
		keys = dnsActionFields
	}
	for _, node := range tree {
		if nested {
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(node.data, &fields)
			key := ""
			if _, present := fields["action"]; present {
				key = "action"
			} else {
				for _, candidate := range keys {
					if _, present := fields[candidate]; present {
						key = candidate
						break
					}
				}
			}
			if key != "" {
				findings = append(findings, ruleError(kind, node.path+"."+key, "nested_rule_action", "sing-box 1.14 does not support actions inside logical child rules. New saves must use top-level actions; review the upgrade diagnostic for legacy state."))
			}
		}
		findings = append(findings, nestedActionFindings(kind, node.children, true)...)
	}
	return findings
}

// PrepareRuleUpgrade handles only the proven nested-action atomic slice.
// In 1.13 children match as HeadlessRule; the router selects only the parent's
// action. Repeated bare reject actions in an explicit reject/domain-only tree
// can therefore be removed with a warning, keeping every predicate, AND/OR,
// invert, sibling and top-level order byte-semantically intact. Actions are
// never lifted. Other action options, empty predicates, response/IP/rule-set
// semantics or unknown shapes need manual review; no partial candidate escapes.
func PrepareRuleUpgrade(config []byte) (RuleCompatibility, error) {
	result := RuleCompatibility{Original: bytes.Clone(config), Candidate: bytes.Clone(config), Outcome: RuleLosslessAutomatic}
	initial := AnalyzeRuleConditions(config)
	var nested []RuleFinding
	for _, f := range initial {
		if f.Code == "nested_rule_action" {
			nested = append(nested, f)
		} else if f.Severity == RuleSeverityError {
			result.Outcome = RuleManualRequired
			result.Findings = initial
			for i := range result.Findings {
				if result.Findings[i].Severity == RuleSeverityError {
					result.Findings[i].MigrationOutcome = RuleManualRequired
					result.Findings[i].OperatorActionRequired = true
				}
			}
			return result, firstRuleError(result.Findings)
		}
	}
	if len(nested) == 0 {
		result.Findings = initial
		return result, nil
	}
	var document map[string]json.RawMessage
	_ = json.Unmarshal(config, &document) // Bounded and structurally checked above.
	manual := false
	for _, kind := range []string{"route", "dns"} {
		section, present := document[kind]
		if !present {
			continue
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(section, &fields)
		roots, present := fields["rules"]
		if !present {
			continue
		}
		elements, _ := rawRuleArray(roots)
		nodes := 0
		tree, _ := readRuleTree(kind, kind+".rules", elements, 1, &nodes)
		changed := false
		for i, node := range tree {
			issues := nestedActionFindings(kind, []rawRule{node}, false)
			if len(issues) == 0 {
				continue
			}
			safe := repeatRejectDomainTree(node, true)
			for _, f := range issues {
				f.MigrationOutcome = RuleManualRequired
				f.OperatorActionRequired = true
				f.Message = "Legacy nested action placement has no proven automatic conversion. Keep the original configuration and repair the indicated rule for sing-box 1.14; operator action is required."
				if safe {
					f.Severity = RuleSeverityWarn
					f.MigrationOutcome = RuleAutomaticDiagnostic
					f.AutomaticAvailable = true
					f.OperatorActionRequired = false
					f.Message = "Legacy child reject action was inactive under the top-level reject action. Automatic conversion can remove only that redundant action when the complete candidate validates; matching, logical structure and order are preserved. Original storage is unchanged."
				}
				result.Findings = append(result.Findings, f)
			}
			if !safe {
				manual = true
				continue
			}
			elements[i] = withoutChildRejectActions(node, true)
			changed = true
		}
		if changed {
			fields["rules"], _ = json.Marshal(elements)
			document[kind], _ = json.Marshal(fields)
		}
	}
	if manual {
		result.Outcome = RuleManualRequired
		return result, firstRuleError(result.Findings)
	}
	candidate, _ := json.Marshal(document)
	findings, err := ValidateRuleConditions(candidate)
	if err != nil {
		result.Outcome = RuleManualRequired
		result.Findings = findings
		for i := range result.Findings {
			result.Findings[i].MigrationOutcome = RuleManualRequired
			result.Findings[i].OperatorActionRequired = true
		}
		return result, firstRuleError(result.Findings)
	}
	result.Candidate = candidate
	result.Outcome = RuleAutomaticDiagnostic
	result.Findings = append(result.Findings, findings...)
	return result, nil
}

func firstRuleError(findings []RuleFinding) error {
	for _, f := range findings {
		if f.Severity == RuleSeverityError {
			return &RuleConditionError{Finding: f}
		}
	}
	return nil
}

func repeatRejectDomainTree(node rawRule, root bool) bool {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(node.data, &fields)
	if action, present := fields["action"]; present {
		var value string
		if json.Unmarshal(action, &value) != nil || value != "reject" {
			return false
		}
	} else if root {
		return false
	}
	var kind string
	_ = json.Unmarshal(fields["type"], &kind)
	logical := kind == "logical"
	condition := false
	for key := range fields {
		switch key {
		case "action", "type", "invert":
		case "mode", "rules":
			if !logical {
				return false
			}
		case "domain", "domain_suffix", "domain_keyword":
			if logical {
				return false
			}
			condition = true
		default:
			return false
		}
	}
	if logical {
		if len(node.children) == 0 {
			return false
		}
		for _, child := range node.children {
			if !repeatRejectDomainTree(child, false) {
				return false
			}
		}
		return true
	}
	return condition
}

func withoutChildRejectActions(node rawRule, root bool) json.RawMessage {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(node.data, &fields)
	if !root {
		delete(fields, "action")
	}
	if _, present := fields["rules"]; present {
		children := make([]json.RawMessage, len(node.children))
		for i, child := range node.children {
			children[i] = withoutChildRejectActions(child, false)
		}
		fields["rules"], _ = json.Marshal(children)
	}
	data, _ := json.Marshal(fields)
	return data
}
