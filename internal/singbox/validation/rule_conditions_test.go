package validation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRuleConditionsPreserveOfficialGrammarAndReportLoss(t *testing.T) {
	for _, test := range []struct {
		name, config, code, path, severity string
	}{
		{"route catch all", `{"route":{"rules":[{}]}}`, "", "", ""},
		{"thin actions", `{"route":{"rules":[{"action":"sniff"},{"invert":true},{"action":"reject"}]},"dns":{"rules":[{"action":"reject"},{"domain":"fixture.example","server":"dns"}]}}`, "", "", ""},
		{"official interface condition", `{"route":{"rules":[{"network_is_expensive":true}]}}`, "", "", ""},
		{"DNS loss", `{"dns":{"rules":[{}]}}`, "decoded_rule_loss", "dns.rules", RuleSeverityWarn},
		{"route logical", `{"route":{"rules":[{"type":"logical","mode":"and","rules":[]}]}}`, "empty_logical_rule", "route.rules[0]", RuleSeverityError},
		// Preserve the 1.13 input; its new incompatibility is owned and localized.
		{"nested DNS logical", `{"dns":{"rules":[{"type":"logical","mode":"or","rules":[{"action":"reject"},{"type":"logical","mode":"and","rules":[]}]}]}}`, "nested_rule_action", "dns.rules[0].rules[0].action", RuleSeverityError},
		{"nested dropped child", `{"route":{"rules":[{"type":"logical","mode":"and","rules":[{}]}]}}`, "empty_logical_rule", "route.rules[0]", RuleSeverityError},
		{"bad mode", `{"route":{"rules":[{"type":"logical","mode":"xor","rules":[{"domain":"fixture.example"}]}]}}`, "invalid_logical_mode", "route.rules[0].mode", RuleSeverityError},
		{"missing mode", `{"dns":{"rules":[{"type":"logical","rules":[{"domain":"fixture.example"}]}]}}`, "invalid_logical_mode", "dns.rules[0].mode", RuleSeverityError},
		{"unknown field", `{"route":{"rules":[{"type":"logical","mode":"and","rules":[{"password-fixture-marker":"secret-fixture-marker"}]}]}}`, "invalid_rule", "route.rules[0].rules[0]", RuleSeverityError},
		{"unknown type", `{"dns":{"rules":[{"type":"secret-fixture-marker"}]}}`, "invalid_rule", "dns.rules[0]", RuleSeverityError},
		{"null root", `{"route":{"rules":[null]}}`, "invalid_rule", "route.rules[0]", RuleSeverityError},
		{"null array", `{"dns":{"rules":null}}`, "invalid_rule_array", "dns.rules", RuleSeverityError},
		{"malformed", `{"route":`, "invalid_json", "config", RuleSeverityError},
	} {
		t.Run(test.name, func(t *testing.T) {
			findings := AnalyzeRuleConditions([]byte(test.config))
			if test.code == "" {
				if len(findings) != 0 {
					t.Fatalf("valid official rule rejected: %#v", findings)
				}
				return
			}
			found := false
			for _, finding := range findings {
				if finding.Code == test.code && finding.Path == test.path && finding.Severity == test.severity {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s at %s: %#v", test.code, test.path, findings)
			}
			encoded, _ := json.Marshal(findings)
			if strings.Contains(string(encoded), "secret-fixture-marker") || strings.Contains(string(encoded), "password-fixture-marker") {
				t.Fatal("finding exposed submitted values or field names")
			}
			_, err := ValidateRuleConditions([]byte(test.config))
			if (err != nil) != (test.severity == RuleSeverityError) {
				t.Fatalf("fatal/warning admission disagrees: %v", err)
			}
			if err != nil {
				var typed *RuleConditionError
				if !errors.As(err, &typed) || typed.Finding.Path == "" || strings.Contains(err.Error(), "secret-fixture-marker") {
					t.Fatal("error lost its safe structural finding")
				}
			}
		})
	}
}

func TestRuleConditionsInclusiveBudgetsBeforeOfficialDecode(t *testing.T) {
	for _, kind := range []string{"route", "dns"} {
		for _, depth := range []int{64, 65} {
			// Old action-bearing equivalents remain immutable upgrade fixtures.
			root := `{"domain":"fixture.example"}`
			for i := 1; i < depth; i++ {
				root = `{"type":"logical","mode":"and","rules":[` + root + `]}`
			}
			config := []byte(fmt.Sprintf(`{"%s":{"rules":[%s]}}`, kind, root))
			_, err := ValidateRuleConditions(config)
			if depth == 64 && err != nil {
				t.Fatalf("inclusive %s depth 64: %v", kind, err)
			}
			if depth == 65 {
				var typed *RuleConditionError
				if !errors.As(err, &typed) || typed.Finding.Code != "rule_depth_limit" {
					t.Fatalf("%s depth limit did not precede official decoding: %v", kind, err)
				}
			}
		}
		for _, count := range []int{4096, 4097} {
			// Mix roots and children so a root-only budget cannot pass this test.
			children := strings.TrimSuffix(strings.Repeat(`{"domain":"fixture.example"},`, count-1), ",")
			config := []byte(fmt.Sprintf(`{"%s":{"rules":[{"type":"logical","mode":"or","action":"reject","rules":[%s]}]}}`, kind, children))
			_, err := ValidateRuleConditions(config)
			if count == 4096 && err != nil {
				t.Fatalf("inclusive %s nodes 4096: %v", kind, err)
			}
			if count == 4097 {
				var typed *RuleConditionError
				if !errors.As(err, &typed) || typed.Finding.Code != "rule_node_limit" {
					t.Fatalf("%s node limit did not include children: %v", kind, err)
				}
			}
		}
	}
	// Independent budgets, not a shared accidental total across two owners.
	rules := strings.TrimSuffix(strings.Repeat(`{"action":"reject"},`, 4096), ",")
	if _, err := ValidateRuleConditions([]byte(fmt.Sprintf(`{"route":{"rules":[%s]},"dns":{"rules":[%s]}}`, rules, rules))); err != nil {
		t.Fatalf("per-kind node budgets: %v", err)
	}
}

func TestDryCheckRejectsInvalidConditionsWithOwnedFinding(t *testing.T) {
	config := []byte(`{"route":{"rules":[{"type":"logical","mode":"and","rules":[]}]}}`)
	var typed *RuleConditionError
	if err := ValidateConfig(config); !errors.As(err, &typed) || typed.Finding.Code != "empty_logical_rule" {
		t.Fatalf("dry check did not use condition owner: %v", err)
	}
}
