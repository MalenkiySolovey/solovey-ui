# Provider maintenance ownership

Source authorities, shared dependency queues and upstream-first copy policy are
defined in [Core repository topology](CORE_REPOSITORY_TOPOLOGY.md). Panel owns
provider selection and qualification; external module source changes belong to
the repository representing that module's actual lineage.

Provider selection is explicit Host installation state. Official remains the
default; ordinary v1 Update requests cannot select an Extended artifact. The
read-only preview binds a signed v2 envelope and explicit artifact set to the
current source and desired-configuration revision. It clears raw configuration
before returning the public projection.

## Installer and release flavors

A fresh installation selects Official (`sing-box`) unless the operator passes
`--core-provider shtorm-extended`. The same choice can be supplied through
`SOLOVEY_UI_CORE_PROVIDER`. Repeated flags, unknown or empty choices fail before
download. Extended fresh installation requires an empty installation with no
existing database or configuration. Importing legacy S-UI state starts with
Official and uses the existing maintenance path for a later provider change.

Provider selection is orthogonal to the existing Solovey platform matrix.
Extended is intended across the Linux, Windows, Docker and OpenWrt targets
already owned by `scripts/release-targets.json` and the canonical OpenWrt target
profiles, including retained OpenWrt-compatible FriendlyWrt ARM64 deployments.
An OS-specific feature limitation belongs in capability facts; it must not
disable the entire Provider or introduce an OS-specific provider identity.

Linux amd64 full/core is the currently qualified Extended subset. The current
release target selections are build availability, not the final R7 platform
acceptance matrix. Remaining production paths require focused cross-build and
package proof, then bounded runtime qualification after semantic parity settles.
Record exact build/dependency/packaging blockers; cross-compilation alone does
not prove installation, lifecycle or physical hardware. R7 completion requires
every existing intended target to be accounted for and platform capability gaps
to preserve stored data. Public stable v2026.4.1 contains no Extended packages.
Use the explicit flag only with a complete published version that supplies the
selected flavor.

Ordinary installer updates observe and retain the installed provider. An
explicit different provider is rejected before network access and points to
the authenticated maintenance command below. An unavailable or ambiguous
installed identity blocks the update. A legacy executable with its exact
Official version output and application metadata is classified only as Official;
that compatibility classification does not establish source provenance.

The requested flavor determines the archive name. After checksum verification
and safe extraction, the installer independently observes the payload's provider
before backups, service changes, database or secret-environment mutation. A
mismatched payload is rejected. Bootstrap uses the existing HTTPS/checksum
contract; the installed Update owner separately requires signed manifests.

Release v2 binds each Panel archive to its provider, complete pinned source,
content digest, architecture and profile. Packaging/signing statically observes
the actual executable and uses the existing Go release owner for canonical
signing bytes and verification. It never runs a candidate executable for that
proof. Recovery retains the original envelope and payload bytes. Official-only
v1 products retain their original recovery contract.

The source disclosure includes both resolved module graphs, their lock and
coordinate digests, each authenticated source checksum and explicit SBOM
dependency edges for each flavor. Shared dependencies are attributed to both;
different engine versions and patches retain separate identities. This complete
source inventory includes tools and platform alternatives and does not claim
that every listed module is linked into each executable.

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

## Administrative CLI

`core-provider` is a thin session client of the existing Update owner. It does
not open the database or maintain its own selection, backup or rollback state.
Use the installed Panel API URL, including its configured base path and `/api`:

```text
solovey-ui core-provider status --api-url https://panel.example/base/api
solovey-ui core-provider preview --api-url https://panel.example/base/api --target-provider shtorm-extended > provider-plan.json
solovey-ui core-provider migrate --api-url https://panel.example/base/api --plan provider-plan.json --confirm MAINTAIN_CORE_PROVIDER_<plan confirmation suffix>
```

`preview` returns the signed target, the preserved configuration observation,
the exact maintenance request and its confirmation. Inspect that plan before
`migrate`; target validation still occurs against the staged executable after
checkpointing and Core admission. The same command supports the explicit
`sing-box` target for a return to Official. `--channel beta` selects the existing
release channel; it does not change the stable parity target or provider policy.

Credentials are JSON records on stdin, requested in order: login
`username`/`password`; when required, login MFA `method`/`credential`; and for
`migrate`, step-up `method`/`credential`. Methods use the existing password,
TOTP or recovery-code rules. Login and step-up require separate fresh MFA
proofs where the existing policy requires them. Supply stdin from a secure
credential prompt or input producer; credentials do not belong in arguments,
environment variables or the plan file. The CLI keeps its cookie jar in memory,
refreshes CSRF after authentication/assurance changes and attempts logout before
exit. It does not print server error bodies, credentials, cookies or grants.
HTTPS uses normal certificate verification. HTTP is limited to a literal
loopback address. Redirects are rejected before forwarding authentication.

The transport is `/v1/operations/update/provider/preview` and
`/v1/operations/update/provider/maintain`. There is no provider Settings switch
or alternate UI mode. The protected command invokes the Semantic Action
`update.provider-maintenance`, permission `update.maintain`, through the same
Update owner and operation correlation as normal Update. The owner rebuilds
the action from all typed plan fields and requires authorization before writes,
downloads, checkpoints or Core hold. Existing administrator/session scope,
CSRF, MFA and one-use step-up checks apply; bearer tokens cannot replace browser
session assurance. Neither a caller-provided authorizer nor command/path/URL
control is accepted in the request. The request audit precedes the owner's
retained SQLite hold, so its HTTP response need not write through that hold.

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
