# Operation coordination

Update and Data lifecycle share stable owner-scoped Change/correlation references
and the atomic `CommitTransition` compare-and-swap plus owner-journal write.
The helper requires the existing operation ID, revision and state; stale or late
completion cannot advance a newer generation. A journal failure commits neither
write. Owner repositories return the previous durable state on failure.

There is no new operation store, executor or recovery engine. Update keeps its
release ledger, provider checkpoints, activation and verified rollback. Data keeps
restore candidate acceptance, exact fallback restoration and drop checkpoints.
Owner-specific multi-row acceptance transactions remain owner-specific.

The read-only Change projection maps existing phases; it is not a persisted state
machine. `RollbackVerified` is asserted only for an owner's accepted ROLLED_BACK
state (including a verified no-mutation closure). Unknown or restored authority
projects RECOVERY_REQUIRED and cannot promote a checkpoint. ExpectedRevision is
the owner's existing binding, such as a restore rehearsal or deployment digest.

On restart Update reconciles interrupted activation through its provider and
checks rollback proof. Data never replays a destructive drop: post-mutation
interruptions require recovery; pre-mutation closures and restored exact fallback
use the owner's existing rules. Owner rows/journals remain the durable truth.
Neither Change IDs nor request cancellation authorize replay or compensation.

API operation fields are preserved with an additive `change` projection. Common
problems contain bounded codes and references, never owner payloads or raw errors.
