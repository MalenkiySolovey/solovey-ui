# Changelog

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
