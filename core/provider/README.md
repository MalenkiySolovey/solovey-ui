# Embedded core source identity

`core/provider` observes the single embedded engine from Go build metadata.
`pins.json` binds the selected replacement module path, exact version and module
checksum to its source commit and semantic baseline. Official remains the default;
the root `go.mod` and `go.sum` still select the accepted Official integration.

The observed identity distinguishes provider version, runtime semantic version,
source repository/commit and artifact state. A matching source pin yields
`state=VERIFIED`; this verifies source metadata only. A Go module checksum is not
a binary digest, so artifact state remains `UNKNOWN` until the existing
Artifact/Trust and Deployment owners provide independent evidence. This catalog
does not qualify a platform, enable a protocol or authorize a provider switch.

Missing metadata, unknown replacements, local source paths, ambiguous modules and
checksum drift yield `UNKNOWN` with reason codes. They never infer Official.
Unrecognized metadata is not reflected into diagnostics. The observed build
identity is immutable for the process; each caller receives its own reason slice.

The existing inbound-control identity separately qualifies bounded fallback
behavior on reviewed source revisions. It reads the actual replacement tuple
through this source catalog, validates the observed uTLS/build profile, and
binds the result to a complete identity digest. The fallback planner consumes
that shared verifier. Research-source recognition alone grants no capability.
QUIC compile facts retain the observed source and module revisions; an unknown
identity cannot acquire support by setting a verified label or nonempty digest.

The server summary, capability snapshot, Operations status, Doctor and CLI use
this owner. Classic and Nexus share the same wire reader and summary component.
The identity is observational: there is no database setting, migration, new
permission or alternate runtime owner. Runtime registration facts remain owned by
the capability registry and must be evaluated separately.

`build.sh --provider shtorm-extended` and
`windows/build-windows.ps1 -Provider shtorm-extended` explicitly select the
separate `go.extended.mod`/`go.extended.sum` graph and `shtorm_extended` native
ABI adapters. Both entrypoints default to Official. The shared build planner
rejects unknown providers, contradictory tags, local replacements and source
pin drift before frontend/backend builds. It builds with a read-only module
lock; neither provider overwrites the other lock.

Extended keeps the native Shtorm semantic baseline and root replacements. Its
maintained engine and QUIC integrations preserve the existing Panel lifecycle,
SSM storage, authenticated parent owner, private API and runtime log wire values.
`deploy/dependencies/sing-box-extended-integration.json` records immutable public
commits, checksums, licenses and complete replayable patches. Verify it with
`node scripts/extended-integration-provenance.mjs check`.

Field capabilities, maintenance transition, artifact trust, semantic/UI parity
and supported-device qualification remain separate R7 gates. Explicit developer
build selection does not make a provider switch or production artifact available.
