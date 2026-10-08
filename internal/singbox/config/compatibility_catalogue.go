package singboxconfig

import "github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"

// BaseCompatibilityCatalogue describes this owner's pinned DNS, HTTP, cache
// and rule consumers. Executable interpretation remains in the same owners.
func BaseCompatibilityCatalogue() []diagnostics.CompatibilityFact {
	return []diagnostics.CompatibilityFact{
		{ID: "DEP-01", Consumer: "dns.servers[].type/address/address_resolver/address_strategy/strategy/detour", Classification: "REMOVED", Policy: "Typed transport conversion only with proven resolver and path; otherwise manual."},
		{ID: "DEP-02", Consumer: "dns.fakeip; legacy fakeip/rcode server addresses", Classification: "REMOVED", Policy: "Convert proven ranges and references; unresolved or final rcode remains manual."},
		{ID: "DEP-03", Consumer: "dns.independent_cache", Classification: "NO_OP", Policy: "Omit with diagnostic about the accepted core's transport-partitioned cache."},
		{ID: "DEP-04", Consumer: "experimental.cache_file.store_rdrc/rdrc_timeout", Classification: "DEPRECATED_ACCEPTED", Policy: "Preserve independent response-rejection persistence; full DNS persistence requires explicit intent."},
		{ID: "DEP-05", Consumer: "DNS ip_cidr/ip_is_private/ip_accept_any; IP-only rule_set/rule_set_ip_cidr_accept_empty", Classification: "DEPRECATED_ACCEPTED", Policy: "Retain valid legacy filter mode; mixed evaluation grammar requires manual correction."},
		{ID: "DEP-06", Consumer: "DNS action strategy; ip_version/query_type/headless query_type", Classification: "DEPRECATED_ACCEPTED", Policy: "Preserve proven legacy mode and evaluation order; no automatic evaluate insertion."},
		{ID: "DEP-07", Consumer: "DNS rule outbound; dialer domain_strategy/domain_resolver; route.default_domain_resolver", Classification: "DEPRECATED_ACCEPTED", Policy: "Retain proven effective resolver semantics; never select a resolver arbitrarily."},
		{ID: "DEP-08", Consumer: "route.rule_set[].download_detour; implicit default HTTP client", Classification: "DEPRECATED_ACCEPTED", Policy: "Freeze proven direct/proxy path in the shared HTTP owner; preserve custom URL and policy."},
		{ID: "DEP-17", Consumer: "route/dns rules geosite/geoip/source_geoip; route.geoip/geosite", Classification: "REMOVED", Policy: "Remove typed inert sentinels; active unpinned datasets require explicit replacement."},
		{ID: "DEP-18", Consumer: "route/dns rules rule_set_ipcidr_match_source", Classification: "REMOVED", Policy: "Normalize only equivalent source-match aliases; divergence is manual."},
		{ID: "DEP-19", Consumer: "experimental.clash_api.cache_file/cache_id/store_mode/store_selected/store_fakeip", Classification: "REMOVED", Policy: "Remove typed zero sentinels; active cache/path/privacy intent requires manual selection."},
		{ID: "DEP-21", Consumer: "experimental.debug.oom_killer", Classification: "REMOVED", Policy: "Only null is inert; non-null cannot authorize the separate oom-killer service."},
		{ID: "DEP-22", Consumer: "V2Ray HTTP/gRPC idle_timeout/ping_timeout; shared HTTP2 keep_alive_period", Classification: "SUPPORTED", Policy: "Preserve each consumer's own fields and duration units; no global rename."},
	}
}
