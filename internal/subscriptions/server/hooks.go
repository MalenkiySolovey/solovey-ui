package server

// Composition-root hooks. They let app wiring connect audit and settings to the
// subscription server without this pure package importing the service layer
// (mirrors the ipmonitor.SecurityEventAuditHook pattern). All are nil until
// wired; every call site nil-checks them, so the package stays usable in tests.
var (
	// ListenFallbackAuditHook records that the subscription listener fell back to
	// loopback because its configured address could not be bound.
	ListenFallbackAuditHook func(component, requestedAddr, fallbackAddr string, bindErr error)

	// SubEnumerationAuditHook records a throttled warning when a source IP crosses
	// the invalid-subscription-id lookup threshold (enumeration/scanning).
	SubEnumerationAuditHook func(ip string, invalidLookups, windowMinutes int)

	// SubRateLimitProvider returns the configured per-IP subscription rate limit.
	// When nil or on error, callers fall back to DefaultRateLimitRequests.
	SubRateLimitProvider func() (int, error)
)
