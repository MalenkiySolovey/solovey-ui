# Bounded common preflight

`Platform` evaluates the existing R2 semantic PlatformFacts snapshot. It validates
the schema, Local scope, content revision, evidence binding and bounded freshness.
There are no additional probes, workers, OS commands or distro decisions.

Owners list critical prerequisites and any required backend. Every listed
prerequisite must be AVAILABLE and current; UNKNOWN, unavailable, cancelled,
malformed or stale facts fail closed. Empty requirements cannot prove readiness.
No generic RAM/disk/package inference is made. R2 unobserved persistent-storage
and package-operation facts remain UNKNOWN. Exact byte/port/checkpoint budgets,
management preservation, signed artifact compatibility and authorization remain
the owner's checks.

Deployment Preview consumes its already observed posture. Existing preview
revision binding remains based on owner semantic inputs and rejection reasons,
not an observation timestamp that would invalidate confirmation every second.
Update gates staging and rechecks before activation through the bounded existing
Deployment PlatformFacts reader. An idempotent terminal replay is still a read;
it does not become a new mutation. Rollback/recovery never loses authority merely
because a forward-operation prerequisite is now unavailable.
