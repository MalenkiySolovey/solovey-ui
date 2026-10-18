# Lightweight core correlation journal

The authenticated operations/journal route reads a bounded index over existing
Update and Data journals. It works without Observability Extra and creates no
global event table, payload copies or migration. Typed events carry the same
CorrelationID, ChangeID, JobID, Local HostRef, owner/resource, operation/revision,
phase/type, safe reason/severity and owner journal reference. SQL selects bounded
metadata prefixes; it never loads checkpoint/config/secret payloads. Invalid
metadata becomes a code or fails the index read, never arbitrary raw detail.

Each request emits at most 200 entries and each owner reads at most limit+1.
Separate monotonically increasing owner sequence cursors advance only for emitted
rows; merge preserves each lane's order when wall time moves backwards. The index
is not a global causal order, a replay log or recovery/verification evidence.
Cursors are local read hints: reset after restore/resynchronization and re-read
the owner operation. Retention gaps do not authorize resuming an old operation.

Retention remains owner-bounded: Update has 128 terminal operations, 1024 journal
entries, a 90-day horizon and an 8 MiB logical budget (protected recovery/current
closure rows retain their existing exceptions). Data has 128 terminal operations,
30 days and 8 MiB logical budget; journals are pruned with their operations.
The index adds no retained data and does not extend those horizons.

Existing Update prepare/activation/rollback, Data Drop and Restore audit records
include the same small typed correlation envelope. Audit remains security truth;
owner transition journals remain durable recovery truth; model.Changes remains
config history; realtime remains bounded acceleration with slow-subscriber drop.
No transition is copied into every sink, and no raw error, secret or arbitrary
payload is added to the common envelope.
