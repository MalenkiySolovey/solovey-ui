# Demonstrated ComponentHost ports

Telegram is the first migrated real consumer. Both app lifecycle composition
and API composition bind `OutboundHTTPClientProvider` and `AuditRecorder` to
their existing Runtime at composition time. Telegram receives neither that
Runtime nor a global Runtime accessor. Other Components keep the current
compatibility surface; this is not a new SDK or capability sandbox.

The transport factory resolves the caller's explicit selected outbound tag and
timeout through the existing core HTTP-client owner. Telegram's existing
direct/proxy transport, per-request outbound client ownership, context
cancellation and idle-connection cleanup remain in its service. Missing or
failed outbound resolution does not select another outbound.

The recorder uses the existing audit owner and event type. Notifier,
scheduled/manual backup and after-commit passphrase-change audits retain their
identity, severity, redaction, error propagation and delivery semantics. The
HTTP-aware audit callback remains available for request audits. Start rejects
missing transport/audit ports before retaining state; a missing manual-backup
recorder fails closed before backup work.

ComponentHost scheduler and Telegram still own job/notifier lifetimes. Stop
drains jobs, then cancels/joins the notifier before unregistering contributions
and releasing HTTP connections. Failed Stop retains the same owner state for
retry. No new worker, scheduler, platform decision or configuration owner is
introduced. Tests demonstrate real composition and Start without broad Runtime.
