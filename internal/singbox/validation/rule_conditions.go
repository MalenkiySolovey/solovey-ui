package validation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	singjson "github.com/sagernet/sing/common/json"
)

const (
	maxRuleDepth = 64
	maxRuleNodes = 4096 // Per DNS/route tree, including roots.

	RuleSeverityError = "error"
	RuleSeverityWarn  = "warn"
)

// RuleFinding contains only fixed diagnostics and structural paths. Official
// decoder errors can contain submitted values and must not cross this boundary.
type RuleFinding = diagnostics.Finding

type RuleConditionError struct{ Finding RuleFinding }

func (e *RuleConditionError) Error() string {
	return fmt.Sprintf("%s [%s]: %s", e.Finding.Path, e.Finding.Code, e.Finding.Message)
}

func ValidateRuleConditions(config []byte) ([]RuleFinding, error) {
	findings := AnalyzeRuleConditions(config)
	for _, finding := range findings {
		if finding.Severity == RuleSeverityError {
			return findings, &RuleConditionError{Finding: finding}
		}
	}
	return findings, nil
}

// AnalyzeRuleConditions bounds the raw tree before invoking the pinned official
// grammar. This is shared by persistence, dry check and Doctor; it is not an
// allowlist of sing-box condition fields or a replacement options parser.
func AnalyzeRuleConditions(config []byte) []RuleFinding {
	var document map[string]json.RawMessage
	if json.Unmarshal(config, &document) != nil || document == nil {
		return []RuleFinding{ruleError("config", "config", "invalid_json", "Configuration must be a JSON object.")}
	}
	var findings []RuleFinding
	for _, kind := range []string{"route", "dns"} {
		section, present := document[kind]
		if !present {
			continue
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(section, &fields) != nil || fields == nil {
			findings = append(findings, ruleError(kind, kind, "invalid_section", "Rule section must be a JSON object."))
			continue
		}
		raw, present := fields["rules"]
		if !present {
			continue
		}
		roots, err := rawRuleArray(raw)
		if err != nil {
			findings = append(findings, ruleError(kind, kind+".rules", "invalid_rule_array", "Rules must be a JSON array."))
			continue
		}
		nodes := 0
		tree, failure := readRuleTree(kind, kind+".rules", roots, 1, &nodes)
		if failure != nil {
			findings = append(findings, *failure)
			continue // Never recursively decode an over-budget tree.
		}
		if nested := nestedActionFindings(kind, tree, false); len(nested) > 0 {
			findings = append(findings, nested...)
			continue
		}
		ruleDocument, _ := json.Marshal(map[string]any{kind: map[string]any{"rules": raw}})
		var options option.Options
		if options.UnmarshalJSONContext(context.Background(), ruleDocument) != nil {
			path := firstDecodeErrorPath(kind, tree)
			if path == "" {
				path = kind + ".rules"
			}
			findings = append(findings, ruleError(kind, path, "invalid_rule", "Rule cannot be decoded by the current sing-box grammar."))
			continue
		}
		if kind == "route" {
			var rules []option.Rule
			if options.Route != nil {
				rules = options.Route.Rules
			}
			findings = inspectRuleTree(findings, kind, kind+".rules", tree, rules, routeConditions)
		} else {
			var rules []option.DNSRule
			if options.DNS != nil {
				rules = options.DNS.Rules
			}
			findings = inspectRuleTree(findings, kind, kind+".rules", tree, rules, dnsConditions)
		}
	}
	return findings
}

type rawRule struct {
	path     string
	data     json.RawMessage
	children []rawRule
}

func rawRuleArray(raw json.RawMessage) ([]json.RawMessage, error) {
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, fmt.Errorf("not an array")
	}
	var elements []json.RawMessage
	err := json.Unmarshal(raw, &elements)
	return elements, err
}

func readRuleTree(kind, prefix string, elements []json.RawMessage, depth int, nodes *int) ([]rawRule, *RuleFinding) {
	tree := make([]rawRule, 0, len(elements))
	for i, element := range elements {
		path := fmt.Sprintf("%s[%d]", prefix, i)
		*nodes++
		if depth > maxRuleDepth {
			finding := ruleError(kind, path, "rule_depth_limit", "Rule tree exceeds the maximum depth of 64.")
			return nil, &finding
		}
		if *nodes > maxRuleNodes {
			finding := ruleError(kind, path, "rule_node_limit", "Rule tree exceeds the maximum of 4096 nodes.")
			return nil, &finding
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(element, &fields) != nil || fields == nil {
			finding := ruleError(kind, path, "invalid_rule", "Rule must be a JSON object.")
			return nil, &finding
		}
		node := rawRule{path: path, data: element}
		if children, present := fields["rules"]; present {
			array, err := rawRuleArray(children)
			if err != nil {
				finding := ruleError(kind, path+".rules", "invalid_rule_array", "Rules must be a JSON array.")
				return nil, &finding
			}
			var failure *RuleFinding
			node.children, failure = readRuleTree(kind, path+".rules", array, depth+1, nodes)
			if failure != nil {
				return nil, failure
			}
		}
		tree = append(tree, node)
	}
	return tree, nil
}

// Localize decoder failures only to known structural nodes, never to arbitrary
// submitted field names or values. Descendants are checked before their parent.
func firstDecodeErrorPath(kind string, tree []rawRule) string {
	for _, node := range tree {
		if path := firstDecodeErrorPath(kind, node.children); path != "" {
			return path
		}
		var err error
		if kind == "route" {
			var rule option.Rule
			err = singjson.Unmarshal(node.data, &rule)
		} else {
			var rule option.DNSRule
			err = singjson.UnmarshalContext(context.Background(), node.data, &rule)
		}
		if err != nil {
			return node.path
		}
	}
	return ""
}

func inspectRuleTree[T any](findings []RuleFinding, kind, prefix string, raw []rawRule, rules []T, inspect func(T) (bool, string, bool, []T)) []RuleFinding {
	if len(raw) != len(rules) {
		findings = append(findings, RuleFinding{Kind: kind, Path: prefix, Code: "decoded_rule_loss", Severity: RuleSeverityWarn, Message: "The current sing-box decoder discarded submitted rules."})
		// The pinned decoder omits all-empty arrays, retaining sibling indices in
		// nonempty arrays. Never attribute a shifted decoded index to raw input.
		if len(rules) != 0 {
			findings = append(findings, ruleError(kind, prefix, "invalid_rule_alignment", "Decoded rules cannot be matched to submitted paths."))
		}
		return findings
	}
	for i, rule := range rules {
		node := raw[i]
		logical, mode, valid, children := inspect(rule)
		if !logical {
			if !valid {
				findings = append(findings, ruleError(kind, node.path, "invalid_conditions", "Rule has no valid conditions."))
			}
			continue
		}
		if len(children) == 0 {
			findings = append(findings, ruleError(kind, node.path, "empty_logical_rule", "Logical rule must contain valid child rules."))
		}
		if mode != C.LogicalTypeAnd && mode != C.LogicalTypeOr {
			findings = append(findings, ruleError(kind, node.path+".mode", "invalid_logical_mode", "Logical rule mode must be and or or."))
		}
		findings = inspectRuleTree(findings, kind, node.path+".rules", node.children, children, inspect)
	}
	return findings
}

func routeConditions(rule option.Rule) (bool, string, bool, []option.Rule) {
	if rule.Type == C.RuleTypeLogical {
		return true, rule.LogicalOptions.Mode, true, rule.LogicalOptions.Rules
	}
	return false, "", rule.DefaultOptions.IsValid(), nil
}

func dnsConditions(rule option.DNSRule) (bool, string, bool, []option.DNSRule) {
	if rule.Type == C.RuleTypeLogical {
		return true, rule.LogicalOptions.Mode, true, rule.LogicalOptions.Rules
	}
	return false, "", rule.DefaultOptions.IsValid(), nil
}

func ruleError(kind, path, code, message string) RuleFinding {
	return RuleFinding{Kind: kind, Path: path, Code: code, Severity: RuleSeverityError, Message: message}
}
