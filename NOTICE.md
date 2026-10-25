# Notices

Solovey UI is a modified GPL-3.0 panel project. The distributed source remains
subject to the repository's GPL-3.0 license and the licenses of its third-party
dependencies. Original copyright, authorship and license notices are retained.
Substantial modifications by MalenkiySolovey include branding, installation and
updates, diagnostics, backup/recovery, release packaging, frontend/backend
behavior, component boundaries and operational security.

Project lineage and retained attribution:

- [alireza0/s-ui](https://github.com/alireza0/s-ui)
- [deposist/s-ui-x](https://github.com/deposist/s-ui-x)
- [admin8800/s-ui](https://github.com/admin8800/s-ui)
- [shenaba/2s-ui](https://github.com/shenaba/2s-ui)
- [printfer/v2sing](https://github.com/printfer/v2sing)
- [Sub-Store](https://github.com/sub-store-org/Sub-Store)

## Networking semantic baseline and actual sources

The official semantic baseline is [SagerNet/sing-box v1.14.2](https://github.com/SagerNet/sing-box/tree/af6e64c3b69e6132ebaee0e1a3d24e93903f6709),
commit `af6e64c3b69e6132ebaee0e1a3d24e93903f6709`, under GPL-3.0-or-later.
The actual resolved source graph includes the three maintained integrations
below. These are patched builds of that semantic baseline. Their pseudo-version
prefixes are Go module resolution metadata.

| Module requested by the product | Actual resolved integration | Exact source commit |
|---|---|---|
| `github.com/sagernet/sing-box@v1.14.2` | `github.com/MalenkiySolovey/sing-box@v1.14.3-0.20261012000247-8b914e4be510` | `8b914e4be5103d5f343dac94f26c3ebeeb226ffc` |
| `github.com/sagernet/sing-quic@v0.7.1-0.20260924092235-6a3a24d65b99` | `github.com/MalenkiySolovey/sing-quic@v0.7.1-0.20261010045634-b2eea8ca8762` | `b2eea8ca8762dd1e438171e2f7b56f9715c30cbb` |
| `github.com/anytls/sing-anytls@v0.0.11` | `github.com/MalenkiySolovey/sing-anytls@v0.0.12-0.20261011014237-8f440972b353` | `8f440972b353f7ace3bbfbac9cf291938de5ab19` |

All three maintained source integrations are GPL-3.0-or-later. Original notices
and authorship remain intact. Integration additions are Copyright (C) 2026
MalenkiySolovey under GPL-3.0-or-later. In particular the sing-quic original
Copyright (C) 2022 nekohasekai remains applicable. The replayable patches and
machine-readable manifests under `deploy/dependencies/` record exact module
sums, original source commits, replacement commits, files and license identity.

- The sing-box cumulative ten-file overlay supplies private SSM cache ownership
  and served-listener lifetime, Naive HTTP/3 parent drain before enclosing UDP
  shutdown, and bounded WebSocket literal-path/early-data handling. Patch
  `sing-box-ssm-cache.patch` has SHA256
  `dad70c774a4f2f2a157254a9982044be082f808ec77f4652fd77eb020d6e72bd`.
  Its legacy filename is retained; `sing-box-ssm-integration.json` describes
  every current scope. Prior SSM-only and Naive-only pins are historical.
- The eight-file sing-quic overlay supplies authenticated parent lifetime,
  graceful close, selective termination and synchronized TUIC early-data close.
  Original source is `6a3a24d65b99587fad1d4cdd567c88f212acdd63`. Patch
  `sing-quic-parent-control.patch` has SHA256
  `fab789e419e1408ac554228ec84ec70aceb2e8975b392dc27658c10d2a0561b5`.
  Protocol verifiers and authentication remain with the original implementations.
  The Naive HTTP/3 integration reuses this exact parent owner and public
  `github.com/sagernet/quic-go@v0.61.0-sing-box-mod.7`; its per-request
  authentication retains flow-level principal controls.
- The four-file AnyTLS overlay synchronizes negotiated peer version and terminal
  stream error using existing atomic types. Original source is v0.0.11 at
  `130d2e61b8895727bfed4942c535e91b246a9603`. Patch
  `sing-anytls-session-state.patch` has SHA256
  `3bec5c9f7772910e2e5a5821a3012a95f460b7328ba7e62282c79acd3f26a64b`.
  Wire format, authentication, constructors, pool policy and lifecycle retain
  their original semantics.

Package build information, the CycloneDX SBOM and release provenance identify
these actual replacements explicitly. Patched binaries are not byte-identical
to unmodified official sing-box v1.14.2.

## Other networking dependencies and native assets

Official sing-box owns the linked protocol implementations, StartedService/API
interfaces and networking semantics. Snell v5/v6 uses the pinned
`github.com/sagernet/sing-snell@v0.0.0-20260829071736-20f2eaec77c3`
under GPL-3.0-or-later. `github.com/godbus/dbus/v5@v5.2.2` supplies local D-Bus
metadata operations under BSD-2-Clause. All remaining selected dependency
versions and replacements are recorded in `go.mod` and `go.sum`.

Cronet native package assets use authenticated
`github.com/sagernet/cronet-go/lib` modules at
`v0.0.0-20260912104006-c10c03c318db` and target-specific SHA256 checks in the
canonical producers. Original Cronet and bundled third-party licenses remain
applicable. The Alpine Docker producer links the pinned static musl asset with
LLVM and records its module, version and hash in `CRONET_INTEGRATION.json`.
Generic Linux and OpenWrt producers also record their distinct pinned native
toolchain authorities. These are packaging integrations of the recorded sources.
The native libraries were built from cronet-go source
`049f2909701ca088c9569f6b303bba5376a2b2ff`, whose Naive/Chromium source is
`SagerNet/naiveproxy@72a06c9fca0e2d228588c7f3074bf7efff3ff686`.
`deploy/dependencies/cronet-native-source.json` binds its complete source archive,
original Chromium/Naive notices and README.chromium-declared third-party license
files. That native source snapshot is included alongside the authenticated Go
modules. The notice archive preserves the original texts and per-project license
metadata, including platform and build alternatives. Retain the applicable BSD,
Apache, LLVM, MPL, APSL and other bundled notices when redistributing native assets.

## Optional Shtorm Extended build flavor

Official remains the default at its accepted v1.14.2 semantic baseline.
The separately compiled `shtorm-extended` flavor uses Shtorm
`v1.14.3-extended-2.7.3`, source
`d9442d14b19ddd02605bf3ee893a171947038e19`, with runtime semantics v1.14.3.
Its actual maintained engine is
`github.com/MalenkiySolovey/sing-box@v1.14.4-0.20261025131044-1343b09f1446`,
commit `1343b09f1446b89e5c3c2986b5893a092b875cb8`. Its maintained QUIC
integration is pinned at `d0ecb33c441a8f188f98bbcf07c0e1d77785e7be`.
`deploy/dependencies/sing-box-extended-integration.json` and its replayable
engine/QUIC patches record the exact bases, source checksums, changes and reasons.
The shared AnyTLS integration retains its separate source identity above.

These engine and QUIC sources carry GPL-3.0-or-later notices. Original authorship
and license bytes remain applicable. The engine's original LICENSE additionally
restricts use of its name or claims of association without consent. Solovey UI
retains its own product name; source/provider attribution does not assert upstream
endorsement. Modified Extended binaries are not byte-identical to upstream
Shtorm or Official sing-box.

The complete source disclosure and SBOM distinguish the two resolved module
graphs, their engine versions and patches, shared modules and provider-specific
dependencies. Original dependency/native notices accompany their authenticated
source archives. Source attribution is separate from platform qualification and
does not establish that every resolved module is linked into each executable.

## Panel behavior references

The accepted original S-UI released target is v1.6.4 at
`db7f9545e7e2907a5d46a2b6bfc4a0b64c2de348`. Historical reference v1.5.3 is
`843c640ba4842d5bde4e179c7da50a3d5052b67c`; primary migration reference v1.6.3
is `13abbdc431ae31ec78a54f5e6643b7fd70b6e634`, with frontend
`691346925b65e485b1cc5c70ca952819bbbe6047` and accepted gitlink
`f859e16953cd733293618f626cc19b8466e00fd3`. Post-v1.6.4 unreleased main is
outside this accepted boundary.

Selected migration and compatibility behavior is independently adapted through
Solovey's existing owners: nested rules and stored state, DNS/cache/HTTP clients,
TLS/ECH providers, the private authenticated runtime API, live sessions and
controls, Snell credentials/admission, mDNS/Resolved, URI/subscription/Clash
projection, shared frontend statistics/cancellation/random utilities and
auto-HTTPS classification. Existing modern Argon2id, authorization, secret
disclosure, component and database ownership are preserved. Original application
implementations and tests were not copied for these selected adaptations.
The original S-UI v1.6.4 SSM and QUIC behavior was a reference; its quicgrace
implementation was not transferred.

The frozen S-UI-X reference is v1.5.12-beta6 at
`08814445d1497bc8b52bdc3af2eda784a5ea26cb`. Independently adapted behavior
includes atomic TCP/UDP admission, token/current-account authorization, checked
imports, database quiescence/restore, shared capability validation and delivery,
paid-subscription invoice identity/provider metadata/recovery, health/history
lifetimes, editor/navigation presentation, Telegram settings and build ownership.
Behavior witnesses include `590b08af39537ab4c85441513743c2360bbc187d`,
`4d2fc76e12a7b972c3126c6000bc57bdd50ec959`,
`9bcf2cd2fc27a995138a419e7566e6f3778ecffe`,
`ccd7cc47437b2caed630f217a30811ecaf4eee88`,
`d5717d95253d4cc1dfbf60894b0c7f08390253ad`,
`42baef5bed45a9a5b91d728e7e2250b524e4a41b` and
`87c1fd049aeb2a7d9046656c801a4e643e73099a`. No implementation, tests or
translations were copied for those independent behavior adaptations.
Earlier networking references v1.13.14/v1.13.18 and sing v0.8.13 belong to
historical integration; current resolved versions are the module manifest's.

Additional firewall, panel, installer and networking projects used for comparison
remain reference-only. A compatible source transfer requires explicit attribution
and license review. Comparison alone does not add their code to this distribution.

## Source and license distribution

The complete release provides the exact project source tree, immutable resolved
Go module archives and npm lockfile package archives, with their original license
and copyright material. The separate dependency-notice archive preserves license,
copyright and notice file bytes and an inventory of their source identity and
SHA256. The SBOM includes build tools and platform alternatives as disclosed
inventory; it does not imply every listed module is linked into every binary.

The three source integrations above include their original license files,
authorship, exact public source revisions and replayable modification notices.
Source-archive identities and dependency checksums accompany release provenance.
Keep these notices and the applicable license material when redistributing the
project, its maintained integrations or bundled third-party assets.
