package singboxconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"

	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/rulepolicy"
	"github.com/sagernet/sing-box/option"
)

// Rule-mode and response-namespace findings mirror the pinned router's
// preconstruction contracts. Its official decoder/construction remain the
// authority for field types and match behavior; no rule is reordered here.
func analyzeDNSRules(root dnsObject, rules []dnsObject, historical bool) []diagnostics.Finding {
	type node struct {
		fields dnsObject
		path   string
		depth  int
	}
	var nodes []node
	for i, rule := range rules {
		nodes = append(nodes, node{rule, fmt.Sprintf("dns.rules[%d]", i), 1})
	}
	var flattened []node
	var findings []diagnostics.Finding
	for len(nodes) > 0 {
		current := nodes[0]
		nodes = nodes[1:]
		if current.depth > rulepolicy.MaxDepth || len(flattened) >= rulepolicy.MaxNodes {
			return append(findings, dnsFailure(current.path, "dns_rule_budget", "DNS rules exceed the supported depth or node budget.", diagnostics.ManualRequired))
		}
		flattened = append(flattened, current)
		var children []dnsObject
		_ = json.Unmarshal(current.fields["rules"], &children)
		for i, child := range children {
			nodes = append(nodes, node{child, fmt.Sprintf("%s.rules[%d]", current.path, i), current.depth + 1})
		}
	}
	legacyPaths := []string{}
	strategyPaths := []string{}
	newPaths := []string{}
	for i, rule := range rules {
		var decoded option.DNSRule
		if err := decoded.UnmarshalJSONContext(context.Background(), dnsJSON(rule)); err != nil {
			findings = append(findings, dnsFailure(fmt.Sprintf("dns.rules[%d]", i), "dns_rule_schema_rejected", "The pinned DNS rule schema rejected this rule. Review its action, condition types and nested action ownership.", diagnostics.ManualRequired))
		}
	}
	for _, current := range flattened {
		rule := current.fields
		response := activeDNSRaw(rule["match_response"])
		if dnsBool(rule, "race") {
			action := dnsString(rule, "action")
			if action != "" && action != "route" && action != "respond" && action != "reject" && action != "predefined" {
				findings = append(findings, dnsFailure(current.path+".race", "dns_race_final_action_required", "Race requires a final route, respond, reject, or predefined action.", diagnostics.ManualRequired))
			}
			if dnsBool(rule, "speculative") {
				findings = append(findings, dnsFailure(current.path+".race", "dns_race_speculative_conflict", "Race and speculative cannot be combined on one rule.", diagnostics.ManualRequired))
			}
			if dnsString(rule, "type") != "logical" && !response {
				findings = append(findings, dnsFailure(current.path+".race", "dns_race_response_required", "Race requires a response context.", diagnostics.ManualRequired))
			}
		}
		for _, key := range []string{"ip_cidr", "ip_is_private", "ip_accept_any"} {
			if !response && activeDNSRaw(rule[key]) {
				legacyPaths = append(legacyPaths, current.path+"."+key)
			}
		}
		if dnsBool(rule, "rule_set_ip_cidr_accept_empty") {
			legacyPaths = append(legacyPaths, current.path+".rule_set_ip_cidr_accept_empty")
		}
		if value := dnsString(rule, "strategy"); value != "" && value != "as_is" {
			strategyPaths = append(strategyPaths, current.path+".strategy")
		}
		for _, key := range []string{"ip_version", "query_type", "match_response", "response_rcode", "response_answer", "response_ns", "response_extra", "race", "speculative", "disable_optimistic_cache"} {
			if activeDNSCondition(rule, key) {
				newPaths = append(newPaths, current.path+"."+key)
			}
		}
		if action := dnsString(rule, "action"); action == "evaluate" || action == "respond" {
			newPaths = append(newPaths, current.path+".action")
		}
		// Inline rule-set metadata is available without reads/downloads. Actual
		// local/remote metadata is supplied by the rule-set construction owner.
		for _, tag := range dnsStringList(rule["rule_set"]) {
			set, found := dnsInlineRuleSet(root, tag)
			if !found {
				if historical {
					findings = append(findings, dnsFailure(current.path+".rule_set", "dns_rule_set_metadata_required", "Pin the referenced rule-set content before upgrading DNS policy. Unknown local or remote metadata cannot prove filtering or internal-query behavior.", diagnostics.ManualRequired))
				}
				continue
			}
			var setRules []dnsObject
			_ = json.Unmarshal(set["rules"], &setRules)
			if historical && (len(setRules) != 1 || dnsBool(setRules[0], "invert") || dnsString(setRules[0], "type") == "logical") {
				findings = append(findings, dnsFailure(current.path+".rule_set", "dns_rule_set_group_manual", "This complex or inverted rule set changes outer match-state sharing. Confirm the intended standalone or grouped policy using pinned rule-set content.", diagnostics.ManualRequired))
			}
			ip, nonIP, qtype := dnsHeadlessFlags(setRules)
			if ip && !nonIP && !response && dnsBool(rule, "rule_set_ip_cidr_match_source") {
				findings = append(findings, dnsFailure(current.path+".rule_set", "dns_pure_ip_source_set_manual", "The pinned core rejects this pure-IP source rule set in current DNS mode. Choose an explicit supported source condition; the original policy is preserved.", diagnostics.ManualRequired))
			}
			if qtype {
				newPaths = append(newPaths, current.path+".rule_set")
			}
			if ip && !response && !dnsBool(rule, "rule_set_ip_cidr_match_source") {
				legacyPaths = append(legacyPaths, current.path+".rule_set")
			}
		}
	}
	if len(newPaths) > 0 && (len(strategyPaths) > 0 || len(legacyPaths) > 0) {
		for _, path := range append(strategyPaths, legacyPaths...) {
			findings = append(findings, dnsFailure(path, "dns_rule_mode_conflict", "Legacy strategy or address filtering conflicts with response or query-based DNS mode. Choose an explicit rule policy without deleting filters or inserting evaluate automatically.", diagnostics.ManualRequired))
		}
		return findings
	}
	if len(strategyPaths) > 0 || len(legacyPaths) > 0 {
		for _, path := range append(strategyPaths, legacyPaths...) {
			findings = append(findings, dnsWarning(path, "dns_legacy_rule_mode_retained", "The pinned core still supports this legacy DNS rule mode. Its filtering and order remain unchanged; a later response-mode conversion requires explicit intent."))
		}
		return findings
	}
	defined := map[string]bool{}
	anonymous := false
	seenRace := false
	for i, rule := range rules {
		path := fmt.Sprintf("dns.rules[%d]", i)
		responseRules := []node{{fields: rule, path: path}}
		for len(responseRules) > 0 {
			entry := responseRules[0]
			current := entry.fields
			responseRules = responseRules[1:]
			response := activeDNSRaw(current["match_response"])
			for _, key := range []string{"ip_cidr", "ip_is_private", "ip_accept_any", "response_rcode", "response_answer", "response_ns", "response_extra"} {
				if activeDNSCondition(current, key) && !response {
					findings = append(findings, dnsFailure(entry.path+"."+key, "dns_response_context_required", "Response matching requires an explicit match_response context and a preceding evaluate action.", diagnostics.ManualRequired))
				}
			}
			if response {
				name := dnsString(current, "match_response")
				if name != "" && !defined[name] {
					findings = append(findings, dnsFailure(entry.path+".match_response", "dns_evaluate_reference_missing", "The response tag must refer to an earlier evaluate action in the same DNS rule namespace.", diagnostics.ManualRequired))
				}
				if name == "" && !anonymous {
					findings = append(findings, dnsFailure(entry.path+".match_response", "dns_evaluate_required", "Anonymous response matching requires an earlier evaluate action without a tag.", diagnostics.ManualRequired))
				}
			}
			if dnsString(current, "action") == "respond" && (dnsString(current, "match_response") == "" || dnsString(current, "type") == "logical") && !anonymous {
				findings = append(findings, dnsFailure(path+".action", "dns_evaluate_required", "This respond action requires an earlier anonymous evaluate response.", diagnostics.ManualRequired))
			}
			if dnsString(rule, "type") == "logical" && dnsString(rule, "action") == "respond" && dnsString(current, "match_response") != "" {
				findings = append(findings, dnsFailure(path+".action", "dns_logical_response_tag", "A logical respond action cannot bind a response tag from child rules.", diagnostics.ManualRequired))
			}
			var children []dnsObject
			_ = json.Unmarshal(current["rules"], &children)
			for i, child := range children {
				responseRules = append(responseRules, node{fields: child, path: fmt.Sprintf("%s.rules[%d]", entry.path, i)})
			}
		}
		if dnsBool(rule, "speculative") && !seenRace {
			findings = append(findings, dnsWarning(path+".speculative", "dns_speculative_without_race", "Speculative evaluation has no effect without a preceding race rule."))
		}
		seenRace = seenRace || dnsBool(rule, "race")
		if dnsString(rule, "action") == "evaluate" {
			if name := dnsString(rule, "tag"); name != "" {
				if defined[name] {
					findings = append(findings, dnsFailure(path+".tag", "dns_evaluate_tag_duplicate", "Evaluate tags must be unique within the DNS response namespace.", diagnostics.ManualRequired))
				}
				defined[name] = true
			} else {
				anonymous = true
			}
			var servers []dnsObject
			var dns dnsObject
			_ = json.Unmarshal(root["dns"], &dns)
			_ = json.Unmarshal(dns["servers"], &servers)
			for _, server := range servers {
				if dnsString(server, "tag") == dnsString(rule, "server") && dnsString(server, "type") == "fakeip" {
					findings = append(findings, dnsFailure(path+".server", "dns_evaluate_fakeip", "Evaluate actions cannot use a FakeIP server.", diagnostics.ManualRequired))
				}
			}
		}
		if activeDNSRaw(rule["ip_version"]) || activeDNSRaw(rule["query_type"]) {
			findings = append(findings, dnsWarning(path, "dns_internal_query_matching_changed", "Query type and IP version now also match internal resolution. Explicit resolver selections continue to bypass this rule selection."))
		}
	}
	return findings
}

func activeDNSCondition(rule dnsObject, key string) bool {
	if key == "response_rcode" {
		raw, present := rule[key]
		return present && string(raw) != "null"
	}
	return activeDNSRaw(rule[key])
}

func dnsStringList(raw json.RawMessage) []string {
	var values []string
	if json.Unmarshal(raw, &values) == nil {
		return values
	}
	var value string
	if json.Unmarshal(raw, &value) == nil && value != "" {
		return []string{value}
	}
	return nil
}
func dnsInlineRuleSet(root dnsObject, tag string) (dnsObject, bool) {
	var route dnsObject
	_ = json.Unmarshal(root["route"], &route)
	var sets []dnsObject
	_ = json.Unmarshal(route["rule_set"], &sets)
	for _, set := range sets {
		if dnsString(set, "type") != "inline" {
			continue
		}
		for _, alias := range dnsStringList(set["tag"]) {
			if alias == tag {
				return set, true
			}
		}
	}
	return nil, false
}
func dnsHeadlessFlags(rules []dnsObject) (ip, nonIP, qtype bool) {
	// Bounded iteration; the aggregate rule validator enforces depth/node budgets.
	for count := 0; len(rules) > 0 && count < rulepolicy.MaxNodes; count++ {
		rule := rules[0]
		rules = rules[1:]
		var children []dnsObject
		_ = json.Unmarshal(rule["rules"], &children)
		rules = append(rules, children...)
		for key, raw := range rule {
			if !activeDNSRaw(raw) {
				continue
			}
			switch key {
			case "type", "mode", "rules", "invert":
			case "ip_cidr":
				ip = true
			case "query_type":
				qtype = true
				nonIP = true
			default:
				nonIP = true
			}
		}
	}
	return
}

// ValidateDNSConfig uses the same reason/path contract for new input. Automatic
// representation repairs are upgrade previews, never silently performed by save.
func ValidateDNSConfig(config []byte) ([]diagnostics.Finding, error) {
	projection, err := prepareDNS(config, false)
	if err != nil {
		return projection.Findings, err
	}
	if !jsonEqualDNS(config, projection.Candidate) {
		for i := range projection.Findings {
			f := &projection.Findings[i]
			if f.Code == "dns_server_migrated" || f.Code == "dns_rcode_migrated" || f.Code == "dns_fakeip_migrated" || f.Code == "dns_fakeip_key_removed" || f.Code == "dns_cache_partition_changed" {
				f.Severity = diagnostics.Error
				f.MigrationOutcome = diagnostics.ManualRequired
				f.OperatorActionRequired = true
				f.AutomaticAvailable = true
			}
		}
		if err := diagnostics.FirstError(projection.Findings); err != nil {
			return projection.Findings, err
		}
	}
	return projection.Findings, nil
}

// CanonicalDNSConfig only clears proven inactive compatibility sentinels after
// strict validation. Active legacy transformations require explicit preview.
func CanonicalDNSConfig(config []byte) ([]byte, error) {
	if _, err := ValidateDNSConfig(config); err != nil {
		return nil, err
	}
	projection, err := prepareDNS(config, false)
	return projection.Candidate, err
}

// Historical query rules used to be skipped during internal resolution. Only
// an explicit per-consumer/default resolver proves the bypass; no resolver is
// invented for an implicit domain dialer. Current typed input is an explicit
// policy choice and still receives the informational mode finding above.
func analyzeInternalDNSReachability(root dnsObject, rules []dnsObject) []diagnostics.Finding {
	var queryPaths []string
	type item struct {
		fields dnsObject
		path   string
	}
	var pending []item
	for i, rule := range rules {
		pending = append(pending, item{rule, fmt.Sprintf("dns.rules[%d]", i)})
	}
	for n := 0; len(pending) > 0 && n < rulepolicy.MaxNodes; n++ {
		current := pending[0]
		pending = pending[1:]
		query := activeDNSRaw(current.fields["query_type"]) || activeDNSRaw(current.fields["ip_version"])
		for _, tag := range dnsStringList(current.fields["rule_set"]) {
			if set, found := dnsInlineRuleSet(root, tag); found {
				var headless []dnsObject
				_ = json.Unmarshal(set["rules"], &headless)
				_, _, q := dnsHeadlessFlags(headless)
				query = query || q
			}
		}
		if query {
			queryPaths = append(queryPaths, current.path)
		}
		var children []dnsObject
		_ = json.Unmarshal(current.fields["rules"], &children)
		for i, child := range children {
			pending = append(pending, item{child, fmt.Sprintf("%s.rules[%d]", current.path, i)})
		}
	}
	if len(queryPaths) == 0 {
		return nil
	}
	var route dnsObject
	_ = json.Unmarshal(root["route"], &route)
	if activeDNSRaw(route["default_domain_resolver"]) {
		return nil
	}
	for _, kind := range []string{"outbounds", "endpoints", "dns"} {
		var rows []dnsObject
		if kind == "dns" {
			var section dnsObject
			_ = json.Unmarshal(root["dns"], &section)
			_ = json.Unmarshal(section["servers"], &rows)
		} else {
			_ = json.Unmarshal(root[kind], &rows)
		}
		for _, row := range rows {
			host := dnsString(row, "server")
			if host == "" || activeDNSRaw(row["domain_resolver"]) {
				continue
			}
			if _, err := netip.ParseAddr(host); err == nil {
				continue
			}
			var findings []diagnostics.Finding
			for _, path := range queryPaths {
				findings = append(findings, dnsFailure(path, "dns_internal_query_policy_manual", "Query rules now affect implicit internal domain resolution. Choose an explicit resolver or confirm the intended internal rule policy before upgrading.", diagnostics.ManualRequired))
			}
			return findings
		}
	}
	return nil
}

func jsonEqualDNS(a, b []byte) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}
	return string(dnsJSON(left)) == string(dnsJSON(right))
}
