package importxui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/deposist/s-ui-x/database"
	"github.com/deposist/s-ui-x/database/model"

	"gorm.io/gorm"
)

func markOnlyNew(plan *MigrationPlan) {
	for i := range plan.Items {
		if plan.Items[i].Conflict {
			plan.Items[i].Action = ActionSkip
		}
	}
}

func planHistorical(ctx context.Context, src *sourceDB, plan *MigrationPlan) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	clients, err := src.dialect.ReadClients(src.sqlDB())
	if err != nil {
		return err
	}
	outbounds, err := src.outboundTraffics()
	if err != nil {
		return err
	}
	count := 0
	for _, row := range clients {
		if row.Email != "" && (row.Up > 0 || row.Down > 0) {
			count++
		}
	}
	for _, row := range outbounds {
		if row.Tag != "" && (row.Up > 0 || row.Down > 0) {
			count++
		}
	}
	preview, err := marshalJSON(map[string]any{
		"client_traffics":   len(clients),
		"outbound_traffics": len(outbounds),
		"mode":              "aggregated_only",
	})
	if err != nil {
		return err
	}
	plan.Items = append(plan.Items, PlanItem{
		Kind:        KindHistory,
		SrcID:       "traffic",
		SrcTag:      "client_traffics/outbound_traffics",
		DstTag:      "stats",
		Action:      ActionCreate,
		PreviewJSON: preview,
		Warnings:    []string{"historical_aggregated_only"},
	})
	plan.Defaults.IncludeHistory = count > 0
	return nil
}

func (s *applyState) applyHistorical(ctx context.Context, tx *gorm.DB, src *sourceDB, opts ApplyOptions) error {
	if !s.hasKind(KindHistory) {
		return nil
	}
	item := s.item(KindHistory, "traffic")
	if item.Action == ActionSkip {
		return nil
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	now := time.Now().Unix()
	if opts.Now != nil {
		now = opts.Now()
	}
	var stats []model.Stats
	clients, err := src.dialect.ReadClients(src.sqlDB())
	if err != nil {
		return err
	}
	for _, row := range clients {
		if row.Email == "" {
			continue
		}
		if row.Up > 0 {
			stats = append(stats, model.Stats{DateTime: now, Resource: "client", Tag: row.Email, Direction: true, Traffic: row.Up})
		}
		if row.Down > 0 {
			stats = append(stats, model.Stats{DateTime: now, Resource: "client", Tag: row.Email, Direction: false, Traffic: row.Down})
		}
	}
	outbounds, err := src.outboundTraffics()
	if err != nil {
		return err
	}
	for _, row := range outbounds {
		if row.Tag == "" {
			continue
		}
		if row.Up > 0 {
			stats = append(stats, model.Stats{DateTime: now, Resource: "outbound", Tag: row.Tag, Direction: true, Traffic: row.Up})
		}
		if row.Down > 0 {
			stats = append(stats, model.Stats{DateTime: now, Resource: "outbound", Tag: row.Tag, Direction: false, Traffic: row.Down})
		}
	}
	if len(stats) > 0 {
		if err := database.CreateInBatchesSafe(tx, &stats); err != nil {
			return err
		}
	}
	s.report.Summary.Historical.Total = len(stats)
	s.report.Summary.Historical.Imported = len(stats)
	s.report.warn("historical_aggregated_only")
	s.progress("historical", "stats")
	return nil
}

// createNewEndpoints persists WARP/wireguard-outbound endpoints, creating each
// only when no endpoint with that tag already exists. It never overwrites an
// existing endpoint, so re-imports and scheduled sync stay idempotent and a
// user-tuned (or same-tagged) endpoint — including its private key — is left
// untouched. The routing rule still references the tag, which now exists either
// way, so there is no dangling reference.
func createNewEndpoints(tx *gorm.DB, endpoints []model.Endpoint, report *Report) error {
	for i := range endpoints {
		ep := &endpoints[i]
		var existing model.Endpoint
		err := tx.Where("tag = ?", ep.Tag).First(&existing).Error
		if err != nil && !database.IsNotFound(err) {
			return err
		}
		if err == nil {
			report.Summary.Endpoints.Skipped++
			report.warn(fmt.Sprintf("endpoint %q already exists; WARP outbound left unchanged", ep.Tag))
			continue
		}
		if err := tx.Create(ep).Error; err != nil {
			return err
		}
		report.Summary.Endpoints.Imported++
		report.warn(fmt.Sprintf("imported WARP endpoint %q from xray wireguard outbound", ep.Tag))
	}
	return nil
}

// createNewOutbounds persists proxy outbounds (vmess/vless/trojan/shadowsocks/
// socks/http) mapped from the source Xray outbounds, creating each only when no
// outbound with that tag already exists. Like createNewEndpoints it never
// overwrites an existing outbound, so re-imports and scheduled sync stay
// idempotent and an operator-tuned (or same-tagged) outbound is left untouched;
// the routing rule still references the tag, which exists either way.
func createNewOutbounds(tx *gorm.DB, outbounds []model.Outbound, report *Report) error {
	for i := range outbounds {
		ob := &outbounds[i]
		var existing model.Outbound
		err := tx.Where("tag = ?", ob.Tag).First(&existing).Error
		if err != nil && !database.IsNotFound(err) {
			return err
		}
		if err == nil {
			report.Summary.Outbounds.Skipped++
			report.warn(fmt.Sprintf("outbound %q already exists; left unchanged", ob.Tag))
			continue
		}
		if err := tx.Create(ob).Error; err != nil {
			return err
		}
		report.Summary.Outbounds.Imported++
		report.warn(fmt.Sprintf("imported %s outbound %q from xray outbound", ob.Type, ob.Tag))
	}
	return nil
}

// planRoutingDisabledNotice surfaces a single warning-only plan item when
// routing import is turned off but the source Xray config still contains proxy
// outbounds or WARP endpoints. Those live in the same xrayConfig and are only
// migrated as part of routing import (an outbound is useless without the rules
// that reference it), so without this notice they would vanish from the
// migration with no plan item and no warning — the exact silent-loss the
// operator hit before this feature existed.
func planRoutingDisabledNotice(ctx context.Context, src *sourceDB, plan *MigrationPlan) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	xrayConfig, err := src.xrayConfig()
	if err != nil {
		return err
	}
	endpoints, outbounds, _, _ := mapXrayOutbounds(xrayConfig)
	if len(endpoints) == 0 && len(outbounds) == 0 {
		return nil
	}
	plan.Items = append(plan.Items, warningOnlyItem(
		KindRouting, "xrayConfig", "xrayConfig.outbounds", "singboxConfig",
		[]string{fmt.Sprintf("%d proxy outbound(s) and %d WARP endpoint(s) in the source are not migrated because routing import is disabled; enable \"Include routing\" to migrate them", len(outbounds), len(endpoints))},
	))
	return nil
}

func planRouting(ctx context.Context, src *sourceDB, plan *MigrationPlan) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	xrayConfig, err := src.xrayConfig()
	if err != nil {
		return err
	}
	endpoints, outbounds, targets, outboundWarnings := mapXrayOutbounds(xrayConfig)
	mapped, warnings, mappedCount, manualCount := MapXrayRouting(xrayConfig, targets)
	warnings = append(outboundWarnings, warnings...)
	preview, err := marshalJSON(mapped)
	if err != nil {
		return err
	}
	action := ActionCreate
	if xrayConfig == "" || (mappedCount == 0 && manualCount == 0 && len(endpoints) == 0 && len(outbounds) == 0) {
		action = ActionSkip
	}
	plan.Items = append(plan.Items, PlanItem{
		Kind:        KindRouting,
		SrcID:       "xrayConfig",
		SrcTag:      "xrayConfig.routing",
		DstTag:      "singboxConfig",
		Action:      action,
		PreviewJSON: preview,
		Warnings:    warnings,
	})
	return nil
}

func (s *applyState) applyRouting(ctx context.Context, tx *gorm.DB, src *sourceDB, _ ApplyOptions) error {
	if !s.hasKind(KindRouting) {
		return nil
	}
	item := s.item(KindRouting, "xrayConfig")
	if item.Action == ActionSkip {
		return nil
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	xrayConfig, err := src.xrayConfig()
	if err != nil {
		return err
	}
	// WARP (and any wireguard outbound) becomes an s-ui endpoint and proxy
	// outbounds become s-ui outbounds; create those first so the routing rules
	// below can target them by tag, then map the rules. blackhole/freedom/dns
	// outbounds resolve to block/direct/hijack-dns.
	endpoints, outbounds, targets, outboundWarnings := mapXrayOutbounds(xrayConfig)
	s.report.warnAll(outboundWarnings)
	if err := createNewEndpoints(tx, endpoints, s.report); err != nil {
		return err
	}
	for i := range endpoints {
		s.progress("endpoints", endpoints[i].Tag)
	}
	if err := createNewOutbounds(tx, outbounds, s.report); err != nil {
		return err
	}
	for i := range outbounds {
		s.progress("outbounds", outbounds[i].Tag)
	}
	mapped, warnings, mappedCount, manualCount := MapXrayRouting(xrayConfig, targets)
	raw, err := marshalJSON(mapped)
	if err != nil {
		return err
	}
	if mappedCount > 0 {
		if err := upsertSetting(tx, firstNonEmpty(item.DstTag, "singboxConfig"), string(raw)); err != nil {
			return err
		}
	}
	s.report.Summary.Routing.Total = mappedCount + manualCount
	s.report.Summary.Routing.Imported = mappedCount
	s.report.Summary.Routing.Skipped = manualCount
	s.report.warnAll(warnings)
	s.progress("routing", "singboxConfig")
	return nil
}

// resolveRoutingTarget maps an Xray outboundTag to an s-ui routing target. The
// targets map is built from the source outbounds (blackhole->block,
// freedom->direct, wireguard/WARP->the endpoint tag). The fallback covers
// configs parsed without an outbounds list.
func resolveRoutingTarget(outboundTag string, targets map[string]string) (string, bool) {
	if t, ok := targets[outboundTag]; ok && t != "" {
		return t, true
	}
	switch strings.ToLower(outboundTag) {
	case "block", "blocked":
		return "block", true
	case "direct":
		return "direct", true
	}
	return "", false
}

func MapXrayRouting(raw string, targets map[string]string) (map[string]any, []string, int, int) {
	result := map[string]any{
		"route": map[string]any{
			"rules":    []any{},
			"rule_set": []any{},
		},
		"dns": map[string]any{},
	}
	if strings.TrimSpace(raw) == "" {
		return result, nil, 0, 0
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return result, []string{fmt.Sprintf("routing: invalid xrayConfig: %v", err)}, 0, 1
	}
	route := result["route"].(map[string]any)
	rulesOut := route["rules"].([]any)
	ruleSets := route["rule_set"].([]any)
	seenRuleSet := map[string]struct{}{}
	mapped := 0
	manual := 0
	var warnings []string
	routing, _ := cfg["routing"].(map[string]any)
	rules, _ := routing["rules"].([]any)
	for index, rawRule := range rules {
		rule, _ := rawRule.(map[string]any)
		if rule == nil {
			continue
		}
		if _, ok := rule["balancerTag"]; ok {
			manual++
			warnings = append(warnings, fmt.Sprintf("routing rule %d uses balancer; manual review required", index))
			continue
		}
		outboundTag := strings.TrimSpace(fmt.Sprint(rule["outboundTag"]))
		if outboundTag == "" {
			manual++
			warnings = append(warnings, fmt.Sprintf("routing rule %d has no outboundTag; manual review required", index))
			continue
		}
		target, ok := resolveRoutingTarget(outboundTag, targets)
		if !ok {
			manual++
			warnings = append(warnings, fmt.Sprintf("routing rule %d outbound %q requires manual review", index, outboundTag))
			continue
		}
		next := map[string]any{}
		if target == dnsHijackTarget {
			// sing-box routes DNS via a rule action, not an outbound.
			next["action"] = "hijack-dns"
		} else {
			next["outbound"] = target
		}
		if domains := stringList(rule["domain"]); len(domains) > 0 {
			for _, domain := range domains {
				if strings.HasPrefix(domain, "geosite:") {
					name := strings.ReplaceAll(domain, ":", "-")
					next["rule_set"] = appendString(next["rule_set"], name)
					if _, ok := seenRuleSet[name]; !ok {
						seenRuleSet[name] = struct{}{}
						ruleSets = append(ruleSets, map[string]any{"tag": name, "type": "remote", "format": "binary"})
					}
					continue
				}
				manual++
				warnings = append(warnings, fmt.Sprintf("routing rule %d domain %q requires manual review", index, domain))
			}
		}
		if ips := stringList(rule["ip"]); len(ips) > 0 {
			for _, ip := range ips {
				if strings.HasPrefix(ip, "geoip:") {
					next["geoip"] = appendString(next["geoip"], strings.TrimPrefix(ip, "geoip:"))
					continue
				}
				next["ip_cidr"] = appendString(next["ip_cidr"], ip)
			}
		}
		if len(next) == 1 {
			manual++
			warnings = append(warnings, fmt.Sprintf("routing rule %d has unsupported matchers", index))
			continue
		}
		rulesOut = append(rulesOut, next)
		mapped++
	}
	route["rules"] = rulesOut
	route["rule_set"] = ruleSets
	if dns, ok := cfg["dns"].(map[string]any); ok {
		result["dns"] = dns
	}
	return result, warnings, mapped, manual
}

func stringList(value any) []string {
	var result []string
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				result = append(result, s)
			}
		}
	case []string:
		result = append(result, v...)
	case string:
		if strings.TrimSpace(v) != "" {
			result = append(result, strings.TrimSpace(v))
		}
	}
	return result
}

func appendString(value any, item string) []string {
	if existing, ok := value.([]string); ok {
		return append(existing, item)
	}
	return []string{item}
}
