# Solovey UI v2026.4.1

This patch publishes the accepted R1–R4 stabilization checkpoint in the existing
2026.4.x line. The networking core remains sing-box 1.14.2, with the accepted
original S-UI v1.6.4 compatibility boundary and the same maintained integrations.

## Fixed

- Administrator and settings CLI commands report a successful persisted change
  without reopening the database and returning a misleading initialization error.
- Server Protection now uses the shared Network Planner for its migrated local
  firewall path. Accepted plans bind resource and input revisions; stale plans
  and known ownership conflicts are rejected before host mutation.
- Planner conflicts retain typed, redacted reasons in API responses instead of
  appearing as an opaque enforcement failure.
- Endpoint changes and resource deletion follow the semantic resource identity.
  Cleanup and verified rollback preserve unrelated local configuration.
- Matching reconciliation avoids unnecessary changes. Recovery remains bounded
  to proven owned resources and waits when prerequisites are unknown.

## Security

- Shared artifact/trust admission and scoped SecretRef access retain their
  canonical owners. The Planner uses the existing typed privileged boundary;
  components do not gain a generic root or network shell.

## Architecture / internal

- Converge component lifecycle, safe removal, local host network facts and
  resource identity with shared Change, Preflight, Health, Jobs and event
  correlation mechanisms. Health evidence binds current revisions and freshness.
- Narrow ComponentHost responsibilities and enforce the accepted architecture
  boundaries with regression ratchets.

## Platform

- Reuse accepted real NanoPi R76S qualification on Armbian and official OpenWrt
  25.12.5, including package/procd lifecycle, fw4 reload, ownership preservation,
  rollback, endpoint change, deletion, reconcile, service restart and reboot.
  The release producer and CLI reporting corrections do not alter those qualified
  network, package, supervision or persistence boundaries.
- OCI config labels and platform/multiarch annotations carry the exact release
  version, source revision and repository, alongside SBOM and provenance.

## Migration notes

- The target core schema is 1.13; v2026.4.0 uses schema 1.12. Preserve a protected
  pre-upgrade backup and use the supported operator-managed installer, package
  or container upgrade. The existing in-app updater rejects a target schema
  different from its running binary's compiled schema. The new binary migrates
  supported state; an older binary must not open the migrated database.
  Downgrade recovery restores the compatible pre-upgrade backup.
- Existing Full/Core profiles and component ownership remain supported. No
  dependency refresh or upstream baseline upgrade is included.

## Known limitations

- Node, the Extended provider and Zapret are not implemented in this release.
- Physical OpenWrt x86 execution remains future targeted platform qualification;
  the R76S result does not establish physical x86 proof or FriendlyWrt OEM proof.
- The existing SEC2 Failover ambiguity remains bounded and deferred. This patch
  does not broaden its accepted guarantees.
