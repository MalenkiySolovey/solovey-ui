package doctor

import "testing"

func TestReferenceChecksFindMissingDNSAndRouteTags(t *testing.T) {
	config := []byte(`{
		"dns":{"servers":[],"final":"missing-dns","rules":[{"server":"missing-rule-dns"}]},
		"route":{"final":"missing-out","rules":[{"outbound":"missing-rule-out"}],"rule_set":[]}
	}`)

	checks := ReferenceChecks(config)
	if !hasItem(checks, "dns-references", SeverityError) {
		t.Fatalf("missing dns reference error: %#v", checks)
	}
	if !hasItem(checks, "route-references", SeverityError) {
		t.Fatalf("missing route reference error: %#v", checks)
	}
}

func TestRuleSetURLChecksWarnsOnUnsafeRemoteURL(t *testing.T) {
	checks := RuleSetURLChecks([]map[string]any{{
		"type": "remote",
		"tag":  "bad",
		"url":  "http://user:pass@example.test/rules.srs",
	}})
	if !hasItem(checks, "ruleset-urls", SeverityWarn) {
		t.Fatalf("missing unsafe URL warning: %#v", checks)
	}
}

func hasItem(items []Item, id string, severity Severity) bool {
	for _, item := range items {
		if item.ID == id && item.Severity == severity {
			return true
		}
	}
	return false
}
