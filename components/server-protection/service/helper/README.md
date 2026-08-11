# Server-protection privileged helper protocol v1

The component uses a narrow child-process boundary for managed nftables
apply/rollback. `solovey-protect-helper` is a separate binary and is not reachable
from an HTTP handler. The panel-side helper client accepts typed Go DTOs only.
There is no command string, shell field, argument array, environment map,
binary path, arbitrary flag or filesystem-root field in the wire protocol.

## Trust boundary

The panel acquires the in-process gate and persisted operation row
before a non-capability call. Immediately before process invocation the client
re-reads and verifies `operation_id`, `instance_id`, lock revision, operation
kind, PID fencing and non-terminal state through `operations.Manager`. A failed
or missing lock returns `missing_capability/operation_lock_required`; it is not
possible to fall back to an unlocked execution path.

The executable is selected only from a trusted install root and must have the
exact name `solovey-protect-helper[.exe]`. It runs without a shell or arguments,
with a fixed environment and the Solovey-managed
`.runtime/server-protection` directory as its working directory. Requests use
root-relative managed paths. Traversal, absolute paths and symlink escapes are
rejected. The helper selects nft binaries and flags internally;
the panel cannot provide them.

The release-sibling executable is hashed when the production client is
constructed and the same regular-file size/SHA-256 identity is re-attested
immediately before every capability and operation invocation. The negotiated
capability revision and pinned helper SHA-256 are returned only as bounded
in-process execution metadata; listener-owner reconciliation binds both into
the owner-observation-set revision. A replacement executable, capability
revision drift, or a helper from a different release therefore fails closed
before it can satisfy a frozen snapshot.

`capabilities`, `ssh.recovery.observe`, and `listener.owner.observe` are
lock-exempt read-only requests. The two observers use fixed, independently
derived production sources and cannot accept a command, PID, proc path,
filesystem path, binary, service unit, or raw flags from the caller. Every
mutating or mutation-adjacent operation requires the persisted lock proof.
Protocol or helper contract mismatch maps to
`missing_capability/helper_version_mismatch`.
The capability payload carries a deterministic revision over the helper,
contract, protocol, nft, and operation facts. Prepare persists that revision;
apply rejects drift before writing or mutating an artifact.

## Typed allowlist

- `capabilities`
- `nft.validate`
- `nft.managed_table.apply`
- `nft.managed_table.rollback`
- `nginx.detect_version`
- `nginx.config.validate`
- `nginx.revision.install`
- `nginx.active.switch`
- `nginx.reload`
- `nginx.active.verify`
- `nginx.revision.restore`
- `listener.probe`
- `listener.owner.observe`
- `ssh.recovery.observe`
- `artifact.manage`

Each operation has exactly one corresponding DTO. Unknown JSON fields,
multiple payloads, unknown enums, arbitrary nft tables, non-loopback listener
probes, unapproved permission modes and oversized artifact content are
rejected. nft operations are restricted to `inet solovey_protection`.

## Listener owner observation

Helper `1.5.1`, contract `1.5`, keeps wire protocol version `1` and adds the
separate read-only `listener.owner.observe` operation. Its input is limited to
one resource ID, configured listen intent (network/mode/address/port), and
exact instance/source/artifact/deployment/runtime-root/resource/config
revisions. Service, executable, PID, cgroup and proc paths are derived from the
root-owned `ApplicationOwnerContractV1`; they are never caller-selected.

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
`nft.validate`, `nft.managed_table.apply`, and
`nft.managed_table.rollback` become available; nginx, listener, artifact,
TTL-set, storm/rate, and hard-block capabilities remain unavailable.

Candidate files must match the deterministic generated grammar, the requested
revision marker, and SHA-256. The grammar can name only
`inet solovey_protection`, its fixed sets/chains, accept-only keep rules, and
`policy accept`; full-ruleset flush, includes, unmanaged tables, arbitrary
statements, raw command text, and path selection are rejected before `nft`
runs. Validation records the exact current managed-table presence, Solovey
revision marker, and SHA-256. Apply refuses any drift from that validated
identity, writes the exact rollback artifact and SHA-256 sidecar, applies a
managed-only transaction, then reads the managed table back and verifies the
revision. Rollback verifies its recorded hash and current-revision fence and
restores/deletes only that table.
`--smoke` remains the only CLI flag.

stdin is limited to 1 MiB. stdout and stderr are independently limited to 256
KiB. The panel applies fixed operation timeouts (15 seconds for ordinary
discovery/validation, a dedicated 60 seconds for exact listener-owner hashing,
60 seconds for apply/reload, and 120 seconds for rollback) and
uses context cancellation to terminate the child process. Exit statuses map to
typed result classes. Audit records contain only correlation, operation,
phase, result code, duration, exit class and truncation flags; payloads, paths,
content, stdout and stderr are never recorded. A redacted audit recorder is
mandatory, and failure to record the pre-invocation attempt prevents the
operation from reaching the helper process.

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

Normal CI uses `MockInvoker`, a fake `NFTExecutor` and a durable fake
`NginxExecutor`; none can start nft or nginx.
The full Linux release packages a release-matched sibling helper, and the panel
wires only that trusted path.
