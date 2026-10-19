# Jobs project owner truth

Update and Data (Restore/Drop) operation APIs expose an additive Job. Job IDs are
stable derivations of the existing owner-scoped correlation identity. No new
durable row, executor, queue, cancellation endpoint or scheduler exists. Cron
remains the periodic scheduler. Read reconnect uses the existing owner operation
and its revision-fenced state; the owner journal remains recovery authority.

Phase is the owner's durable phase. Update progress is verified/staged bytes,
not overall activation percentage; Data exposes phases without invented byte or
percentage counters. Owner adapters identify mutation/verification/rollback
critical sections. Common code does not infer them from arbitrary payloads.

The current APIs expose no durable cancellation request, so cancelSupported is
false and cancelRequested is absent. Existing request contexts remain cooperative
owner input. Cancellation during preflight cannot admit work. Cancellation after
durable commit cannot change COMMITTED to cancelled or roll back acceptance.
No generic process or goroutine kill exists. There is no invented deadline or
retry guarantee. Provider/app shutdown and startup reconciliation remain owned
by their original lifecycles, documented in README.md.

Successful Update staging commits the complete byte count with DOWNLOADED so
the response and reconnect agree. Progress callbacks can update only their
original DOWNLOADING revision; late, negative or duplicate completions cannot
change a newer/terminal operation. State revision and bytes have distinct roles:
bytes can advance within a phase without creating an artificial state transition.

Problems/results contain bounded codes and opaque references, never raw errors,
secrets, file paths or artifact payloads. Restored/untrusted rows project recovery
with no progress or verified checkpoint authority.
