# Changelog

## 2026.3.2-beta.9

- Server Protection's Systemd runtime authority now separates persistent backing
  identity and access policy from filesystem tuning emitted by a particular boot.
  This repairs the reproduced clean beta.8 cold broker registration failure when
  `mb_optimize_scan=0` disappears from mountinfo after reboot.
- Device, filesystem, source, resolved backing path, writable state and superblock
  access/security policy remain checked. The complete bound process-local mount
  proof and installed owner generation are still rechecked before use.
- Added the exact beta.8 negative-control regression, production cold handler
  registration, real Systemd namespace binding and adversarial backing checks.
  Physical beta.8-to-beta.9 acceptance remains separate; stable stays 2026.3.1.

## 2026.3.2-beta.8

- Native installation registers the existing tmpfiles policy for boot, preserving
  root:panel 0750 socket-parent ownership after volatile runtime state is cleared.
  This repairs the reproduced EACCES before first-request broker activation.
- Backups, failed-install rollback, management rollback and uninstall include the
  boot registration. Panel capabilities and broker authentication are unchanged.
- Native regression now installs through the production owner and checks boot
  policy discovery and two authenticated Status calls before Doctor/SQLite gates.
  Physical beta.7-to-beta.8 acceptance remains separate; stable stays 2026.3.1.

## 2026.3.2-beta.7

- Deployment posture persistence uses a write-first atomic UPSERT, removing the
  real WAL read-to-write upgrade race reproduced as SQLITE_BUSY_SNAPSHOT (517).
  Desired/generated intent and durable Doctor authority remain preserved.
- Authenticated persistence diagnostics expose only bounded stage, storage class
  and SQLite codes. Required state and snapshot writes remain fail-closed.
- Doctor retention preserves its current revision target within the existing
  64-snapshot bound, including duplicate or older report replay.
- Added real WAL, systemd broker-to-API, package-managed and Docker persistence
  regressions. Physical beta.6-to-beta.7 upgrade acceptance remains separate;
  stable/latest remains 2026.3.1.

## 2026.3.2-beta.6

- Linux broker clients accept the root PID1 identity of systemd-created Unix
  listeners. Previously the client closed before sending its request, leaving
  the broker with an empty receive and unavailable Deployment/SSH/Update paths.
- Request receive failures retain closed, payload-free diagnostic classes.
  Rejected truncated descriptor transfers now close every delivered descriptor.
  Frame bounds, strict JSON, per-request writer checks and pidfd liveness remain.
- CI now exercises real socket activation with a nonroot production client,
  production server/attestor and the unchanged broker capability policy across
  three cold starts and 24 requests. CAP_KILL remains absent.
- Physical Debian13 beta.5-to-beta.6 upgrade and clean-install qualification
  remain separate gates. Stable/latest remains 2026.3.1.

## 2026.3.2-beta.4

- Native installation explicitly publishes root-owned, service-group-readable
  component packs and inventory. Secret creation no longer leaks its umask;
  installer-managed inventory disables unsupported panel install/remove writes.
- Native broker startup binds sealed runtime mount authority to the equivalent
  systemd sandbox backing directory, retaining strict later mount rechecks.
  Startup failures expose bounded owner-local stages without nested error data.
- Core Linux releases now include a minimal broker that does not require absent
  optional component authority. Added real nonroot component migration/routes,
  systemd namespace and full broker graph/socket regression coverage.

## 2026.3.2-beta.2

- Fixed native application-owner validation rejecting correct Linux filesystem
  ownership because it used the wrong stat type. Identity publication now sets
  exact metadata before rename, with safe path and expected/actual diagnostics.
- Failed native installations restore previous files, DB presence, service and
  socket activity, and persistent/runtime enablement. Snapshots follow writer
  quiescence; ordinary errors and termination signals roll back. Interrupted or
  incomplete recovery retains a durable fence against unsafe retries.
- Added real Linux ownership and systemd rollback regressions. SSH proof's exact
  dedicated-group 2755 contract and OpenWrt/FriendlyWrt lifecycles are preserved.

## 2026.3.2-beta.1 (historically published as 2026.3.2)

- Fixed native Linux installation rejecting its own SSH reconnect proof helper.
  Installer, broker manifest, live peer attestation and native updates now agree
  on root ownership, the exact dedicated socket group and setgid permissions.
  Existing root:root executable trust remains strict.
- Added real Linux installer/manifest and setgid socket regression gates to CI
  and release qualification, including unsafe ownership and permission cases.
- Included post-2026.3.1 persistence-policy build isolation, capability-aware
  qualification, and synchronous restore/session-rotation audit completion.

## 2026.3.0

- Added the disabled-by-default Server Protection component with endpoint and
  host-surface inventory, capability-aware planning, fronting, firewall
  composition, UDP guard, recovery, and auditable operation workflows.
- Added hardened native deployment profiles and a constrained privileged
  broker boundary, plus deployment and SSH-management surfaces and explicit
  Docker networking contracts.
- Strengthened panel security with step-up verification, session and realtime
  protections, stricter request validation and budgets, safer secret handling,
  and expanded security audit reporting.
- Added signed update metadata, stronger update and rollback coordination,
  streamed backup protection, restore rehearsal, durable ownership checks, and
  explicit component data-lifecycle operations.
- Consolidated component registration, manifests, commands, settings, backup
  codecs, health and resource contracts so full and core profiles share one
  deterministic composition model without optional imports in core builds.
- Expanded the frontend for security, deployment, operations, SSH management,
  and component-owned routes and locales, with additional accessibility and
  profile checks.
- Preserved upgrade compatibility and existing core panel behavior with
  broader migration, installer, packaging, architecture, and regression tests.
- Kept both Windows x64 and ARM64 release archives on native runners with the
  required CGO-backed SQLite runtime; Linux and Docker coverage is unchanged.
- Kept host-bound advanced protection modes experimental or inspection-only
  where separate external acceptance is still required.

## 2026.2.0

- Added the component runtime model: optional features register routes, jobs,
  settings, database hooks, frontend entries, and lifecycle behavior only when
  installed and enabled.
- Added component-aware install profiles and release packaging: full binary,
  core binary, compact component bundle, and release manifest.
- Added panel update UI support for component availability, version
  compatibility, enable/disable, install/remove, and explicit data deletion.
- Improved remote outbound subscriptions: normalized collected profile data,
  group conversion, delay checks, bulk group operations, and synchronization
  rules.
- Improved frontend drag-and-drop selection behavior and UI consistency across
  Nexus and classic layouts.
- Updated release, Docker, and local development scripts for the modular build.

## 2026.1.0

- Introduced the Solovey UI versioning line.
- Reworked remote subscription parsing and synchronization.
- Added Windows development helpers and release hardening.
