# Solovey UI 2026.5.0

This feature release brings the accepted R5–R7 work together after v2026.4.1.
Official sing-box remains the default. Shtorm Extended is an explicit second
CoreRuntime provider for the same panel, configuration owners and UI.

## Highlights

- Choose Shtorm Extended on a fresh installation. Existing installations retain
  their provider during ordinary upgrades. Later provider changes use the
  authenticated maintenance workflow with capability preview, checkpoint,
  validation, verification and rollback.
- Configure additional native protocols and transports in the existing Inbounds,
  Outbounds, DNS and TLS editors. Classic and Nexus share state, permissions,
  validation and apply behavior.
- Use expanded subscription and import workflows. Unsupported provider state is
  preserved with explicit activation blockers, without silent field loss or a
  replacement Direct outbound.

## New features

- Extended native coverage includes Mieru, TrustTunnel, Sudoku, SSH/MTProxy and
  Call, plus provider-native Bond, CoreFailover, MASQUE, XHTTP, VPN endpoints and
  Realm functionality where the selected build supports them. Editors and client
  delivery use the actual provider capability set.
- Managed WireGuard/AmneziaWG users and devices retain stable identity,
  credential ownership, address allocation and private provisioning semantics.
- External subscription sources support inline data and local-file snapshots,
  include/exclude filters, name transforms, private fetch headers, explicit
  download outbounds, dial overrides and observed source health.
- Typed ComponentHost/ComponentKit ports have real Telegram and Observability
  consumers. Semantic Actions bind authorization to the owning operation and
  target. URL-scoped Local Host context keeps tabs and shared UI contributions
  consistent.
- The versioned `solovey.management/v1` contract and cross-repository conformance
  fixtures establish a management boundary for future Node development.

## Improvements

- Provider identity, source revision and capabilities follow packages,
  build-info, Health and maintenance. Unavailable types are excluded from
  Create/Add while historical desired state remains inspectable.
- EndpointIR, subscription rendering and import planning preserve native options,
  secret references and explicit unsupported-format diagnostics.
- User identity, runtime accounting, quota resets, disable/revoke and subscription
  token rotation retain distinct owners and effects.
- Official and Extended have separate Linux Full/Core archives, Windows Full/Core
  ZIPs, OpenWrt APKs and multiarchitecture container flavors. Source disclosure,
  notices and SBOM retain their separate dependency graphs.

## Fixed

- Logical Backup/Restore preserves stored false, zero and empty values instead
  of reapplying database defaults during typed export (PR #216).
- Creating a fallback site with explicit `enabled=false` keeps it disabled
  (PR #217).
- Correct Windows native-library packaging, SQLite/runtime readiness and private
  install permissions; complete OpenWrt static composition and Docker provider
  packaging.
- Preserve owned runtime retirement, credential handling and rollback after
  failed provider activation.

## Security

- Retain deny-by-default authorization, session/MFA-bound step-up, SecretRef
  handling, nonroot Panel execution and the bounded privileged broker.
- Carry the accepted compression dependency security correction and exact
  maintained source pins. Signatures, checksums and attribution remain required.
- Historical SEC-2 omitted-Failover-final ambiguity remains bounded and deferred;
  this release does not claim to resolve it.

## Platform support

- Linux: amd64, arm64, armv7, armv6, armv5, 386 and s390x; Full/Core for both
  providers. Naive/Cronet is unavailable on armv5, armv6 and s390x. Other native
  features remain subject to the build's capabilities.
- Windows: amd64/arm64 Full/Core packages, with native hosted and emulated
  evidence. This does not establish physical Windows ARM64 hardware proof or a
  general production Windows server support promise.
- Containers: Linux amd64/arm64, separate Official and Extended image tags.
- OpenWrt 25.12.5: x86/64 and rockchip/armv8 APKs for both providers. x86 runtime
  proof uses KVM; ARM64 hardware proof uses NanoPi R76S.
- FriendlyWrt 25.12.5 on the qualified R76S/vendor 6.1.141 kernel uses the same
  ARM64 APK and the canonical direct persistent `/opt` descriptor. Accepted
  hardware proof covers package/procd/broker, real TCP/UDP, encrypted logical
  Backup/Restore with nonempty owner files, provider roundtrip and normal reboot
  persistence. Arbitrary vendor images, firmware replacement, raw eMMC contents
  and power-loss durability are outside that proof.

## Installation and upgrading

- Previous stable: v2026.4.1. Core schema remains **1.13 → 1.13**. Keep a
  protected compatible logical backup before maintenance.
- A fresh native installation defaults to Official; select Extended explicitly
  with `--core-provider shtorm-extended`. Existing state uses the supported
  provider maintenance flow; changing an installer flag is not a migration.
- Full/Core selection and component inventory remain deployment-owned. A full
  binary does not by itself enable or activate every optional component.
- Signed in-app updates retain schema, provider and deployment admission rules.
  Package/container installations use their operator-managed lifecycle.
  Recovery to an older binary requires compatible state and backup.
- v2026.4.1 predates the provider-aware signed release v2 format. Upgrade that
  version through the operator-managed installer/package/container path; its
  installed updater correctly rejects the newer envelope format.

## Known limitations

- Bridge network integration is deferred and unavailable. It is absent from
  Create/activation; historical Bridge state remains preserved.
- Zapret and production Node are not implemented. The management contract and
  Local Host foundation do not provide a remote Node agent.
- Native features depend on platform/build capabilities. Build, native runtime,
  KVM/emulated and physical proof are distinct; all 23 Extended profiles are not
  claimed physically tested.

The signed release envelope covers the generic Linux/component update set.
Windows ZIPs, APKs, the FriendlyWrt descriptor and disclosure assets have their
checksum and canonical producer/provenance contracts outside that scope.
