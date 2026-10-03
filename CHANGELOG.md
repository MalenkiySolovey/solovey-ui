# Changelog

## Unreleased

- Improved stable native/systemd and package/procd deployments, persistent storage admission, cold broker activation and recovery across supported Linux platforms.
- Preserved nonroot panel execution, zero effective capabilities and kernel no_new_privs enforcement, including package-managed deployments.
- Improved durable Deployment Status and Doctor persistence, backup/restore owner registration and all eight optional component lifecycles.
- Hardened browser request serialization against inherited Axios options while preserving the panel's normal form wire format.
- Cleaned product documentation and reusable validation tooling without changing qualified platform, storage, privilege or update-trust contracts.

## 2026.3.2-beta.10

- Enforce and verify kernel no_new_privs before package-managed panel and broker handoff; syscall failures prevent launch.

## 2026.3.2-beta.9

- Preserve native runtime mount authority across equivalent filesystem tuning and recover installer-owned services from failed start limits.
- Retain exact backing, ownership, access and process-generation checks before broker-backed operations.

## 2026.3.2-beta.8

- Register the packaged native tmpfiles policy for every boot and preserve its state through install rollback and uninstall.

## 2026.3.2-beta.7

- Avoid SQLite WAL read-to-write contention when persisting Deployment Status and Doctor observations.
- Keep bounded persistence diagnostics and preserve the current Doctor report during history retention.

## 2026.3.2-beta.6

- Support root PID1 peers on systemd-created broker listeners while retaining per-request credential and pidfd checks.
- Close descriptors delivered with rejected truncated transfers and retain bounded receive diagnostics.

## 2026.3.2-beta.4

- Publish service-group-readable native component packs and retain broker authority across equivalent systemd sandbox mounts.
- Include a minimal broker in core releases without requiring absent optional component authority.

## 2026.3.2-beta.2

- Correct native filesystem-owner validation and publish exact metadata before identity-file rename.
- Restore prior database presence, service/socket activity and enablement on failed installations; interrupted recovery fails closed.

## 2026.3.2-beta.1

- Align native installation, manifest creation, peer attestation and updates with the dedicated-group setgid SSH proof helper contract.
- Preserve persistence-policy isolation and durable restore/session-rotation audit completion.

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
