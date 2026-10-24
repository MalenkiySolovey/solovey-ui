# Provider maintenance ownership

Provider selection is explicit Host installation state. Official remains the
default; ordinary v1 Update requests cannot select an Extended artifact. The
read-only preview binds a signed v2 envelope and explicit artifact set to the
current source and desired-configuration revision. It clears raw configuration
before returning the public projection.

The maintenance entry point uses the existing Update operation and journal.
Network staging precedes SQLite maintenance admission. Under that admission,
ConfigService captures the same canonical owners used by save/apply. Update
publishes the immutable Backup checkpoint and commits its reference in the
existing PREFLIGHTING row/journal before requesting the root checkpoint or
holding CoreRuntime. No provider table or second rollback journal is added.

Root's fixed target check binds the exact executable, Panel version, provider
source, unchanged configuration bytes and canonical build capability snapshot.
The same fixed command independently checks the authenticated previous
executable against that input and its claimed version/capability revision. Root
records both report digests and a bounded, sorted capability comparison.
Missing facts remain UNKNOWN; an explicit loss remains UNSUPPORTED with its
owner reason. Neither state authorizes stripping stored options. Unknown or
rejected critical configuration blocks activation and retains the checkpoint.
Build capability evidence excludes environment readiness. The installed runtime
must independently restore the prior maintenance intent and prove its source,
executable, configuration, capabilities, generation and existing health checks.
Rollback keeps the same checkpoint until the old executable has passed that
runtime verification; reverting links alone does not complete rollback.

Provider-only transitions currently require the same compiled Panel version.
They retain the live database on rollback, preserving traffic observations and
unsupported stored fields. A changed desired-configuration revision blocks an
operator rollback without releasing an already retained maintenance admission.
At startup a pending authenticated root checkpoint requires a hold even when
the application's corresponding history is missing. Missing history cannot
reconstruct trusted application authority or synthesize successful recovery.
A Panel version change requires the canonical Backup/Restore
maintenance path and is rejected by this entry point until that integration is
qualified. This boundary is not a claim that the full R7 lifecycle is complete.

## Root recovery and service control

Maintenance revision `update-broker-maintenance-1.2` additionally requires the
installed Root recovery binding. A provider-only activation requires the actual
kernel-selected running broker executable and the target generation's broker
to be root-owned, trusted executable objects with identical content digests and
sizes. The same check precedes target configuration validation and activation.
Different broker bytes require the ordinary update lifecycle first. Source or
Panel version equality cannot substitute for executable equality.

Only the Panel service restarts during this transition. The unchanged Root
process retains the existing Update checkpoint, observes the original durable
activation timestamp and rolls back after the shared five-minute health
deadline if no Panel can verify the candidate. Observation cannot refresh that
deadline. Background rollback uses the same checkpoint implementation as the
authenticated rollback verb; it has no Panel peer and cannot claim management
readiness or runtime verification. The predecessor must still pass the normal
Panel health and configuration checks before rollback completes.

The service port opens the trusted systemctl executable once per operation,
revalidates that same object, resets start-limit failure for fixed services and
requests a nonblocking restart. Provider recovery names only
`solovey-ui.service`. Ordinary release updates name that service and
`solovey-privileged-broker.service`. No request supplies a unit, PID, command,
path, environment or argument list. This works with the declared broker
capabilities, which do not include CAP_KILL. The previous cross-UID signal path
could neither stop the Panel with those capabilities nor restart a cleanly
terminated service configured with Restart=on-failure.

Recovery retries a failed predecessor restart with a bounded delay until its
original recovery deadline. An unavailable controller retains the checkpoint
and requires repair; it cannot mark recovery verified. Pruning retains the one
generation containing the running shared broker until a later broker restart.

## Worker lifetime review

The two added foundation-budget sites are individually reviewed additions; the
R1 baseline identity and legacy inventory remain unchanged.

- `APP.startUpdateMaintenanceWatchdog` owns exactly one launch after web,
  subscriptions, component startup and initial Update reconciliation. App stop
  cancels and drains it before stopping those services or clearing owner hooks.
  Repeated starts cannot launch a second worker. Drain is bounded at 35 seconds.
- `LifecycleManager.RunMaintenanceWatchdog` owns a two-second ticker, stops it
  on exit, and does work only while Update retains the existing SQLite
  maintenance admission. Each reconciliation has a 30-second child context and
  attempts the same manager lock without waiting for staging. It keeps that
  lock throughout reconciliation, with the same state machine, journal and root
  checkpoint. No new queue, transaction manager or rollback mechanism is
  introduced.
- Connected privileged-broker I/O is closed on caller cancellation, so an RPC's
  larger wire timeout cannot outlive the app worker. The client cancellation
  test joins both ends of a blocked response wait. The Update cancellation test
  joins the watchdog even while another operation holds its manager lock.
  Normal and rollback runtime probes get fresh operation
  fences, while deadlines remain anchored to their original journal entries.
- Terminal runtime acceptance ends the SQLite admission even when terminal
  cache cleanup leaves recorded cleanup debt. Ambiguous authority stays held
  for recovery; it cannot silently start a different provider or restore stale
  database contents.

The Root service binding admits three additional, individually reviewed budget
sites, preserving the existing R1 budget and observations:

- The existing `run` authority watcher launch now closes a completion channel.
  All broker exits cancel and join that worker. It receives the one bound
  Update runtime, also used by the production handler registrar.
- Its existing ticker moves to `watchAuthorityManifestWithMaintenance` and is
  stopped on exit. The Update callback has a 30-second child context and skips
  a busy owner. RPC handlers use that same owner lock with context-cancelable
  admission. The callback starts no worker and owns no additional journal.
  A recovery failure produces only a fixed safe diagnostic and does not stop
  client-authority reload or fabricate successful recovery.
- The existing finite `scheduleReleaseRestart` launch retains the reply grace,
  then executes fixed service control with a twenty-second parent deadline.
  It no longer signals a caller-selected PID or kills its own process.

This is source/unit ownership evidence. Actual executable restart, supported
platform/package qualification, Classic/Nexus workflow coverage and the full R7
semantic/UI parity matrix remain separate acceptance gates.
