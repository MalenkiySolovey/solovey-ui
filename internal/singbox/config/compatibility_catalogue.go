package singboxconfig

import "github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"

// BaseCompatibilityCatalogue describes this owner's pinned DNS, HTTP, cache
// and rule consumers. Executable interpretation remains in the same owners.
func BaseCompatibilityCatalogue() []diagnostics.CompatibilityFact {
	return []diagnostics.CompatibilityFact{
		{"DEP-01", "dns.servers[].type/address/address_resolver/address_strategy/strategy/detour", "REMOVED", "Typed transport conversion only with proven resolver and path; otherwise manual."},
		{"DEP-02", "dns.fakeip; legacy fakeip/rcode server addresses", "REMOVED", "Convert proven ranges and references; unresolved or final rcode remains manual."},
		{"DEP-03", "dns.independent_cache", "NO_OP", "Omit with diagnostic about the accepted core's transport-partitioned cache."},
		{"DEP-04", "experimental.cache_file.store_rdrc/rdrc_timeout", "DEPRECATED_ACCEPTED", "Preserve independent response-rejection persistence; full DNS persistence requires explicit intent."},
		{"DEP-05", "DNS ip_cidr/ip_is_private/ip_accept_any; IP-only rule_set/rule_set_ip_cidr_accept_empty", "DEPRECATED_ACCEPTED", "Retain valid legacy filter mode; mixed evaluation grammar requires manual correction."},
		{"DEP-06", "DNS action strategy; ip_version/query_type/headless query_type", "DEPRECATED_ACCEPTED", "Preserve proven legacy mode and evaluation order; no automatic evaluate insertion."},
		{"DEP-07", "DNS rule outbound; dialer domain_strategy/domain_resolver; route.default_domain_resolver", "DEPRECATED_ACCEPTED", "Retain proven effective resolver semantics; never select a resolver arbitrarily."},
		{"DEP-08", "route.rule_set[].download_detour; implicit default HTTP client", "DEPRECATED_ACCEPTED", "Freeze proven direct/proxy path in the shared HTTP owner; preserve custom URL and policy."},
		{"DEP-17", "route/dns rules geosite/geoip/source_geoip; route.geoip/geosite", "REMOVED", "Remove typed inert sentinels; active unpinned datasets require explicit replacement."},
		{"DEP-18", "route/dns rules rule_set_ipcidr_match_source", "REMOVED", "Normalize only equivalent source-match aliases; divergence is manual."},
		{"DEP-19", "experimental.clash_api.cache_file/cache_id/store_mode/store_selected/store_fakeip", "REMOVED", "Remove typed zero sentinels; active cache/path/privacy intent requires manual selection."},
		{"DEP-21", "experimental.debug.oom_killer", "REMOVED", "Only null is inert; non-null cannot authorize the separate oom-killer service."},
		{"DEP-22", "V2Ray HTTP/gRPC idle_timeout/ping_timeout; shared HTTP2 keep_alive_period", "SUPPORTED", "Preserve each consumer's own fields and duration units; no global rename."},
	}
}
