# Server-protection privileged helper protocol v1

The component contributes narrow semantic operations to the single production
`solovey-privileged-broker`. The panel-side client accepts typed Go DTOs only.
There is no command string, shell field, argument array, environment map,
binary path, arbitrary flag or filesystem-root field in the wire protocol.

## Trust boundary

The panel acquires the in-process gate and persisted operation row
before a non-capability call. Immediately before broker invocation the client
re-reads and verifies `operation_id`, `instance_id`, lock revision, operation
kind, PID fencing and non-terminal state through `operations.Manager`. A failed
or missing lock returns `missing_capability/operation_lock_required`; it is not
possible to fall back to an unlocked execution path.

The broker entrypoint and managed root are selected by deployment policy; an
HTTP request cannot select either. Requests use
root-relative managed paths. Traversal, absolute paths and symlink escapes are
rejected. The helper selects nft binaries and flags internally;
the panel cannot provide them.

The broker client re-attests the fixed socket and root peer credentials on
every connection. The negotiated capability revision and broker identity
revision are returned only as bounded in-process execution metadata;
listener-owner reconciliation binds both into the owner-observation-set
revision. Capability or broker contract drift therefore fails closed before
it can satisfy a frozen snapshot.

`capabilities`, `nft.managed_table.observe`, `ssh.recovery.observe`, and
`listener.owner.observe` are lock-exempt read-only requests. The observers use
fixed, independently derived production sources and cannot accept a command,
PID, proc path, filesystem path, binary, service unit, table, or raw flags from
the caller. Every
mutating or mutation-adjacent operation requires the persisted lock proof.
Protocol or helper contract mismatch maps to
`missing_capability/helper_version_mismatch`.
The capability payload carries a deterministic revision over the helper,
contract, protocol, nft, and operation facts. Prepare persists that revision;
apply rejects drift before writing or mutating an artifact.

## Typed allowlist

- `capabilities`
- `nft.validate`
- `nft.managed_table.observe`
- `nft.managed_table.apply`
- `nft.managed_table.rollback`
- `nginx.detect_version`
- `nginx.config.validate`
- `nginx.revision.install`
- `nginx.active.switch`
- `nginx.reload`
- `nginx.active.verify`
- `nginx.revision.restore`
- `listener.owner.observe`
- `ssh.recovery.observe`

Each operation has exactly one corresponding DTO. Unknown JSON fields,
multiple payloads, unknown enums, arbitrary nft tables, invalid managed paths,
invalid revision identities, and oversized requests are rejected. There is no
generic artifact or filesystem mutation operation. nft operations are
restricted to `inet solovey_protection`.

## Listener owner observation

Helper `1.11.0`, contract `1.11`, keeps wire protocol version `1`; dated nft
candidates reuse the helper's absolute-expiration materialization. The contract
contains only the typed semantic operations listed above. The earlier
test-oriented generic artifact vocabulary is not part of this boundary. The
separate read-only `listener.owner.observe` operation has input limited to
one resource ID, configured listen intent (network/mode/address/port), and
exact instance/source/artifact/deployment/runtime-root/resource/config
revisions. Service, executable, PID, cgroup and proc paths are derived from the
root-owned `ApplicationOwnerContractV1`; they are never caller-selected.

`ssh.recovery.observe` is only a protocol adapter over
`sshbroker.RecoveryObserver`. The SSH owner alone selects the resolved daemon,
service-control and log-evidence adapters, observes configuration posture, and
returns bounded fresh public-key facts with verifier and observer revisions.
Server Protection supplies only the time window/cardinality policy and rejects
an incomplete, stale, noncanonical, duplicated, or out-of-order projection.

On Linux the observer opens the exact systemd MainPID with `pidfd_open`,
duplicates at most 4,096 process FDs through `pidfd_getfd`, and verifies the
socket with `getsockname`, inode, `SO_COOKIE`, `SO_ACCEPTCONN` and
`IPV6_V6ONLY`. Before and after that bounded scan it verifies process start
ticks, UID/GID, executable canonical path/hash/device/inode, systemd
unit/MainPID/fragment/active state/cgroup/start identity, and the active
deployment contract. Ambiguity, missing kernel capability, drift, stale
service state, and contract mismatch return typed reason codes and no weakened
owner fact. A successful `ListenerOwnerFactV1` is short-lived; freshness is
revalidated on every preview/prepare/apply, while its semantic observation
revision changes when the socket/process/service/deployment identity changes.
The operation has a dedicated bounded 60-second window because the active
executable SHA-256 is recomputed rather than trusted from a process name or
path; deadline/cancellation is returned as a typed helper failure, never
misclassified as owner drift.

The production HostSurface provider declares an 80-second outer reconciliation
budget: at most 15 seconds for capability negotiation, 60 seconds for the
typed owner operation, and five seconds of bounded orchestration overhead.
Application-resource observations run concurrently with a maximum of four
workers; each resource is invoked exactly once and there is no retry or reuse
across resource, configuration, contract, helper, or deployment changes.

This proof establishes endpoint ownership only. It does not create a
RecoveryPath or firewall exemption, and the operation has no nft executor
path.

## Nft execution behavior

The helper detects Linux, `nft` from fixed system locations, and bounded
read-only nft netlink access. Unknown
platform, version, or primitive support remains unavailable. Only
`nft.validate`, `nft.managed_table.observe`, `nft.managed_table.apply`, and
`nft.managed_table.rollback` become available; nginx, listener, TTL-set,
storm/rate, and hard-block capabilities remain unavailable.

Candidate files must match the deterministic generated grammar, the requested
revision marker, candidate-artifact SHA-256, static semantic identity, and
timed-membership identity. The grammar can name only
`inet solovey_protection`, its fixed sets/chains, accept-only keep rules, and
`policy accept`; full-ruleset flush, includes, unmanaged tables, arbitrary
statements, raw command text, and path selection are rejected before `nft`
runs. A recovery exemption is an accept rule with a bounded absolute
`meta time <` deadline; only the separately authenticated trusted-source
exemption is permanent. Candidate numeric epochs and nft's quoted UTC listing
form normalize to one semantic expression. `solovey-ui/managed-nft-semantic/v1`
deterministically identifies the
static policy: table/object identities, revision marker, set type/flags/size/
default timeout, chain hook/priority/policy, and ordered rule expressions and
verdicts. Text formatting, rule handles, counter packet/byte values, and
decreasing remaining element lifetime are excluded. The separate
`solovey-ui/managed-nft-timed-membership/v1` identity covers the exact members
of every owned timeout set. Unexpected insertion and premature loss therefore
drift, while declared natural expiry is removed from the expected membership
at its absolute deadline. A duplicate or malformed managed object fails closed.

Validation freshly observes exact managed-table presence, revision, and both
semantic identities. Apply refuses static or timed-membership drift from the
pre-mutation observation. Untimed state retains the bounded raw rollback
artifact. Timed state is captured as a private authenticated rollback
projection containing each member's original absolute deadline; rollback
materializes only members whose deadline is still in the future, so retry,
delay, and restart cannot extend or resurrect them. Both forms retain a
SHA-256 sidecar. Apply then runs a managed-only transaction and freshly
observes and proves the target identities. Rollback verifies its recorded
artifact hash and both current semantic fences, restores/deletes only that
table, then freshly proves the previous identities or absence. Candidate
SHA-256, live static semantic SHA-256, live timed-membership SHA-256, and
rollback-artifact SHA-256 are separate propositions throughout the protocol.
Generated candidates are admitted only through the shared 256 KiB ceiling;
the mandatory live reader and rollback capture have a 512 KiB ceiling for nft
formatting, handles, counters, and remaining-TTL expansion. Oversized plans are
rejected before any helper call or mutation, and a real namespace gate proves
that near-limit accepted state remains observable.
The broker protocol bounds each payload to 1 MiB. The panel applies fixed operation timeouts (15 seconds for ordinary
discovery/validation, a dedicated 60 seconds for exact listener-owner hashing,
60 seconds for apply/reload, and 120 seconds for rollback) and
uses context cancellation to terminate the request. Audit records contain only correlation, operation,
phase, result code, duration, exit class and truncation flags; payloads, paths,
content, stdout and stderr are never recorded. A redacted audit recorder is
mandatory, and failure to record the pre-invocation attempt prevents the
operation from reaching the broker.

## Managed-table lifecycle

The durable firewall composition is never treated as proof that the volatile
kernel table still exists. Component startup observes the table before
presenting current authority. OpenWrt firewall lifecycle events accelerate the
same observation by restarting the panel instance, and an owner-local
one-minute schedule is the bounded lost-event fallback on Linux. Neither path
replays a candidate. An absent table atomically retires the active composition,
its contributions, and its rollback transitions under the current operation
revision fence; the operation becomes `forgotten`, so status is truthful and a
new apply may establish a fresh generation.

An exact replay of an already applied operation rebinds the immutable request
and desired plan, recomposes the current revision, and freshly observes live
nft authority. It succeeds without a helper mutation only when all identities
still match. A different valid plan or missing/drifted live state is rejected;
persisted `APPLIED` alone is never a success result.

Ordinary process stop preserves fail-safe firewall state. Exact OpenWrt package
removal is the terminal ownership event: after procd has stopped the package
processes, the root lifecycle authenticates and deletes only
`table inet solovey_protection`, verifies absence, and only then removes the
runtime tree. Absence and repeated cleanup are successful; a same-name foreign
table or a failed delete is visible and fences runtime cleanup.

## Nginx execution behavior

The nginx backend becomes available only on Linux when one allowlisted binary,
stream plus ssl_preread support, the strict root-owned managed nginx subroot,
fixed loader, exact existing active revision/hash and rollback listeners are
verified. API/UI cannot select the binary, managed root, loader, executable
arguments, signal, service, user config or arbitrary destination.

Candidate validate and install requests name only a managed-root-relative
artifact already written by the panel. The engine independently checks the
exact bytes/SHA-256 and rejects traversal or any symlinked segment. Validation
uses a run-scoped managed prefix and fixed `nginx -t -q` arguments with bounded
timeout/output and redacted diagnostics. Successful validation never changes
the active reference.

Install publishes `nginx/revisions/<revision>` by fsync and rename. Switch and
restore atomically replace only the controlled `active` symlink. Reload uses
the fixed managed loader and a durable operation/revision intent/result, so a
retry cannot signal twice after a completed reload. Verify requires the exact
active revision/hash, unchanged detected binary identity, master and worker
executables, and socket ownership for every expected listener. User
`nginx.conf`, `conf.d`, `sites-enabled`, `sites-available` and arbitrary
includes are never read, copied or mutated.

Unit coverage uses `MockInvoker`, a fake `NFTExecutor` and a durable fake
`NginxExecutor`; none can start nft or nginx.
Production exposes this protocol only through the privileged broker; there is
no standalone helper process adapter or helper executable in the source,
build, release package, installer, or runtime composition.
