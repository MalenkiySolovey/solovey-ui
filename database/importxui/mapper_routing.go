package importxui

import (
	"fmt"
	"strconv"
	"strings"
)

// applyRuleMatchers translates an Xray routing rule's matcher fields into
// sing-box route-rule fields on next. It returns whether at least one matcher
// was added (a rule with none cannot be a sing-box rule) and warnings for
// fields that can only be partially represented.
//
// Caller handles the un-representable matchers (attrs, balancerTag) by marking
// the whole rule manual before calling this — dropping them would silently
// broaden the match.
func applyRuleMatchers(index int, rule, next map[string]any, ruleSets *[]any, seen map[string]struct{}) (bool, []string) {
	added := false
	var warnings []string

	if domains := stringList(rule["domain"]); len(domains) > 0 {
		matched, unknown := mapDomainMatchers(domains, next, ruleSets, seen)
		if matched {
			added = true
		}
		for _, d := range unknown {
			warnings = append(warnings, fmt.Sprintf("routing rule %d domain %q has an unsupported prefix; that entry was dropped", index, d))
		}
	}

	destAdded, destGeoip := mapIPMatchers(rule["ip"], next, ruleSets, seen, false)
	srcAdded, srcGeoip := mapIPMatchers(rule["source"], next, ruleSets, seen, true)
	if destAdded || srcAdded {
		added = true
	}
	if srcGeoip {
		// A geoip rule set matches the destination IP by default; this flag makes
		// the rule's IP-CIDR rule sets match the source instead.
		next["rule_set_ip_cidr_match_source"] = true
		if destGeoip {
			warnings = append(warnings, fmt.Sprintf("routing rule %d mixes source and destination geoip; rule_set_ip_cidr_match_source applies to all of them — review manually", index))
		}
	}
	if mapPortMatchers(rule["port"], next, "port", "port_range") {
		added = true
	}
	if mapPortMatchers(rule["sourcePort"], next, "source_port", "source_port_range") {
		added = true
	}
	if nets := splitCSVList(rule["network"]); len(nets) > 0 {
		next["network"] = nets
		added = true
	}
	if protos := stringList(rule["protocol"]); len(protos) > 0 {
		next["protocol"] = protos
		added = true
	}
	if inbounds := stringList(rule["inboundTag"]); len(inbounds) > 0 {
		next["inbound"] = inbounds
		added = true
	}
	if users := stringList(rule["user"]); len(users) > 0 {
		next["auth_user"] = users
		added = true
	}
	return added, warnings
}

// mapDomainMatchers translates Xray domain matcher entries (geosite:/domain:/
// full:/regexp:/keyword:/bare) into sing-box domain fields on dst (shared by
// route rules and DNS rules). It returns whether any matcher was added and any
// entries with an unsupported prefix (e.g. ext:) that were dropped.
func mapDomainMatchers(domains []string, dst map[string]any, ruleSets *[]any, seen map[string]struct{}) (bool, []string) {
	added := false
	var unknown []string
	for _, d := range domains {
		switch {
		case strings.HasPrefix(d, "geosite:"):
			code := geoRuleSetCode(strings.TrimPrefix(d, "geosite:"))
			if code == "" {
				continue
			}
			tag := "geosite-" + code
			dst["rule_set"] = appendString(dst["rule_set"], tag)
			registerRemoteRuleSet(ruleSets, seen, tag, fmt.Sprintf(geositeRuleSetURLFmt, code))
			added = true
		case strings.HasPrefix(d, "full:"):
			dst["domain"] = appendString(dst["domain"], strings.TrimPrefix(d, "full:"))
			added = true
		case strings.HasPrefix(d, "regexp:"):
			dst["domain_regex"] = appendString(dst["domain_regex"], strings.TrimPrefix(d, "regexp:"))
			added = true
		case strings.HasPrefix(d, "keyword:"):
			dst["domain_keyword"] = appendString(dst["domain_keyword"], strings.TrimPrefix(d, "keyword:"))
			added = true
		case strings.HasPrefix(d, "domain:"):
			dst["domain_suffix"] = appendString(dst["domain_suffix"], strings.TrimPrefix(d, "domain:"))
			added = true
		case strings.Contains(d, ":"):
			unknown = append(unknown, d)
		default:
			// A bare domain is matched by Xray as a domain (and its subdomains).
			dst["domain_suffix"] = appendString(dst["domain_suffix"], d)
			added = true
		}
	}
	return added, unknown
}

// Remote rule-set sources. sing-box removed the inline geoip/geosite route
// matchers in 1.12, so a geoip/geosite match is migrated to a remote rule set
// pointing at the MetaCubeX meta-rules-dat repository — the same source s-ui's
// own subscription/rule-set tooling uses.
const (
	geositeRuleSetURLFmt = "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/%s.srs"
	geoipRuleSetURLFmt   = "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/%s.srs"
)

// mapIPMatchers maps Xray ip/source entries: a geoip:* becomes a remote geoip
// rule set referenced by the rule, a bare value becomes an ip_cidr (or
// source_ip_cidr) matcher. It returns whether anything was added and whether a
// geoip rule set was used (so the caller can set source matching).
func mapIPMatchers(value any, next map[string]any, ruleSets *[]any, seen map[string]struct{}, source bool) (added bool, geoipUsed bool) {
	cidrKey := "ip_cidr"
	if source {
		cidrKey = "source_ip_cidr"
	}
	for _, ip := range stringList(value) {
		if strings.HasPrefix(ip, "geoip:") {
			code := geoRuleSetCode(strings.TrimPrefix(ip, "geoip:"))
			if code == "" {
				continue
			}
			tag := "geoip-" + code
			next["rule_set"] = appendString(next["rule_set"], tag)
			registerRemoteRuleSet(ruleSets, seen, tag, fmt.Sprintf(geoipRuleSetURLFmt, code))
			added = true
			geoipUsed = true
		} else {
			next[cidrKey] = appendString(next[cidrKey], ip)
			added = true
		}
	}
	return added, geoipUsed
}

// registerRemoteRuleSet appends a remote rule-set definition to ruleSets the
// first time a tag is seen, so route/DNS rules can reference it. sing-box
// requires a url and format on a remote rule set.
func registerRemoteRuleSet(ruleSets *[]any, seen map[string]struct{}, tag, url string) {
	if _, ok := seen[tag]; ok {
		return
	}
	seen[tag] = struct{}{}
	*ruleSets = append(*ruleSets, map[string]any{
		"tag":             tag,
		"type":            "remote",
		"format":          "binary",
		"url":             url,
		"download_detour": "direct",
	})
}

// geoRuleSetCode normalises an Xray geoip/geosite code into the lowercase token
// used by the rule-set repository, dropping any "@attribute" suffix (which the
// .srs sets do not carry).
func geoRuleSetCode(raw string) string {
	code := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.IndexByte(code, '@'); i >= 0 {
		code = code[:i]
	}
	return strings.TrimSpace(code)
}

// mapPortMatchers splits Xray port specs (a number, an "a-b" range, or a list/
// comma string of either) into a sing-box single-port list and a port-range
// list ("a:b"). Returns whether anything was added.
func mapPortMatchers(value any, next map[string]any, portKey, rangeKey string) bool {
	added := false
	for _, token := range portTokens(value) {
		if lo, hi, ok := splitPortRange(token); ok {
			next[rangeKey] = appendString(next[rangeKey], fmt.Sprintf("%d:%d", lo, hi))
			added = true
			continue
		}
		if p, err := strconv.Atoi(token); err == nil && p >= 0 && p <= 65535 {
			next[portKey] = appendInt(next[portKey], p)
			added = true
		}
	}
	return added
}

// portTokens flattens an Xray port value (number, string, comma list, or array)
// into individual port/range tokens.
func portTokens(value any) []string {
	var out []string
	add := func(s string) {
		for _, part := range strings.Split(s, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	switch v := value.(type) {
	case nil:
		return nil
	case []any:
		for _, item := range v {
			add(strings.TrimSpace(fmt.Sprint(item)))
		}
	case string:
		add(v)
	default:
		add(strings.TrimSpace(fmt.Sprint(v)))
	}
	return out
}

// splitPortRange parses "lo-hi" into its bounds.
func splitPortRange(token string) (int, int, bool) {
	i := strings.IndexByte(token, '-')
	if i <= 0 || i >= len(token)-1 {
		return 0, 0, false
	}
	lo, err1 := strconv.Atoi(strings.TrimSpace(token[:i]))
	hi, err2 := strconv.Atoi(strings.TrimSpace(token[i+1:]))
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return lo, hi, true
}

// splitCSVList flattens a value that may be a comma string ("tcp,udp"), a single
// string, or an array into a trimmed list.
func splitCSVList(value any) []string {
	var out []string
	for _, s := range stringList(value) {
		for _, part := range strings.Split(s, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// appendInt appends an int to a value that is either nil or an existing []int.
func appendInt(value any, item int) []int {
	if existing, ok := value.([]int); ok {
		return append(existing, item)
	}
	return []int{item}
}
