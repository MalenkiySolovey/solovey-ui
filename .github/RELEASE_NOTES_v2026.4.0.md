# Solovey UI 2026.4.0

This release moves the networking core to the official sing-box 1.14.2 semantic
baseline and completes the accepted compatibility boundary through released
original S-UI v1.6.4. Solovey's component, privilege and storage contracts remain
in place.

- Stored configuration migrates to core schema 1.12 through the existing staged
  validation and database transaction owners. Migration preview identifies
  settings that need operator correction; failed acceptance retains protected
  state and recovery information.
- DNS and cache migration, HTTP client references, rule-set downloads, TLS/ECH
  providers and certificate state use consistent save, runtime and export
  validation. mDNS and systemd-resolved are available where their platform and
  authorization requirements are satisfied.
- CoreRuntime owns a private authenticated runtime API with bounded operations,
  credential redaction and generation-scoped lifecycle. Live sessions, flow
  disconnect, deliberate maintenance, outbound groups and log controls consume
  actual runtime state in both classic and Nexus interfaces.
- Hysteria, Hysteria2 and TUIC support authenticated parent lifecycle, graceful
  close and selective termination. Parent identity includes the principal,
  owning inbound and runtime generation. Naive HTTP/3 drains its own parents
  during removal, replacement and shutdown; shared per-request authentication
  retains flow-level user controls.
- AnyTLS synchronizes concurrent session state. Snell v5/v6 validation, managed
  credentials, multiuser admission and shared editors follow the pinned core.
- URI, sing-box JSON and Clash/Mihomo export preserve accepted IPv6, credential,
  WebSocket path/early-data, public ECH and transport semantics. AES-128 AEAD2022
  credentials use the same authenticated identity across save, render and export.
- Shared frontend editors, statistics refresh, cancellation, traffic ordering,
  random utilities and TLS/DNS controls follow the accepted original S-UI
  behavior while retaining Solovey's component ownership and two interface modes.
- Auto-HTTPS classification preserves stream data and caller deadlines. Native
  backup/restore returns bounded truthful results, fences retired database
  generations and rebinds owners. Full-to-Core and custom-profile installation
  handle an intentionally absent optional component catalog.
- Packaging provides Linux Full/Core for the seven existing architectures,
  Windows amd64/arm64 Full/Core, Docker amd64/arm64, both existing OpenWrt APK
  targets and the shared component bundle. FriendlyWrt uses the accepted
  OpenWrt-compatible APK and storage descriptor.

The core uses disclosed maintained integrations for SSM cache/listener lifetime,
Naive HTTP/3 and bounded WebSocket handling, authenticated QUIC parents, and
AnyTLS session state. Exact versions, source commits, replayable patches and
license notices accompany the source, CycloneDX SBOM and provenance assets.
These builds are patched integrations of the official 1.14.2 semantic baseline.
Pseudo-version prefixes are module resolution identities; the upstream semantic
targets remain sing-box 1.14.2 and AnyTLS 0.0.11.

Before upgrading from v2026.3.3, retain a verified private backup of the database,
configuration, certificates and component state, and review migration findings.
The target schema is 1.12; recovery to the older release requires its protected
pre-upgrade state. Do not open an already migrated database with an older binary.
Use the normal native installer, package or container update path to cross from
published v2026.3.3/schema 1.11 to this release. That published updater retains
its exact-schema admission boundary. Signed self-update metadata starts at
2026.4.0/schema 1.12; manual recovery is declared. Package-managed and Docker
deployments continue to use their operator-managed update paths.

Accepted runtime and security qualification is retained for unchanged owners,
including native systemd, Windows, Docker and the OpenWrt-compatible ARM
deployment scopes. OpenWrt x86/64 has build, package and source qualification;
physical x86 testing remains future targeted qualification and was not executed.

Every downloadable artifact has a SHA-256 sidecar. The existing signed update
envelope authorizes the generic Linux/component set. Windows, APK, source,
license, SBOM and provenance assets have separately verified checksums; Docker
images retain their platform identity, SBOM and build provenance.
