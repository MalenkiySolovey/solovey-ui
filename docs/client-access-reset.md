# Client access during traffic reset

Core schema 1.13 adds the private `clients.access_disable_reason` column to
the existing Client owner. The public `enable` field and Client IDs retain
their existing API shape. The reason cannot be supplied through JSON/form
client payloads. Explicit administrative Client saves set manual disable
intent (or explicitly enable through the existing save action).

Depletion records `QUOTA_EXHAUSTED` or `EXPIRED`, preferring expiry when both
apply. Scheduled reset clears only quota exhaustion, checks the current
expiry with the same `< now` boundary as depletion, and preserves every
other reason, including unknown future revocation reasons. A manual traffic
reset never enables a Client. Lifetime totals use current counters and the
reset update is fenced by the current reset period and policy.

Older disabled rows have no reliable provenance. The journaled migration
classifies them as `LEGACY_DISABLED`; neither usage counters nor change-feed
history proves quota-only intent. They stay disabled until an explicit
administrative save. Disabled X-UI imports receive the same classification;
merge imports preserve destination access state. Backup/restore retains the
private column. Schema 1.12 backups migrate through the same conservative
boundary; older binaries reject schema 1.13 rather than opening it. The
historical 1.12 migration contract/checksum is unchanged.

Paid renewal still extends quota/expiry through its existing transaction,
but preserves manual, legacy, and other stronger blockers. This is the same
Client access authority, not a separate payment policy owner. The model has
no independent per-credential revocation subsystem; reset treats any such
persisted reason conservatively if present.

Release compatibility metadata describes the new source schema boundary;
this change does not publish a release or change the stable tag.
