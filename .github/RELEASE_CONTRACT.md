# Release Policy

Version source of truth:

- `config/identity/version` contains the application release version.
- The value must be SemVer-shaped as `YYYY.RELEASE.PATCH[-PRERELEASE]`.
- Solovey UI release numbers are independent from upstream S-UI/S-UI-X
  versions. Use year-based SemVer: `YYYY.RELEASE.PATCH[-PRERELEASE]`.
- The value must not include a leading `v`.
- The value must not include build metadata.
- Prerelease identifiers must be lowercase SemVer identifiers.

Git release tags:

- Git tag names use `v` plus the exact `config/identity/version` value.
- Example: `config/identity/version` = `2026.2.0`, Git tag = `v2026.2.0`.

Database version policy:

- `settings.version` records the newest application version that successfully
  migrated or adapted the database.
- `settings.version` must never be downgraded by an older binary.
- Legacy database values with only `MAJOR.MINOR` are accepted for comparison
  and treated as `MAJOR.MINOR.0`.

Release checklist:

- Update `config/identity/version`.
- Add `Unreleased` changelog entries before cutting the tag, then move them
  under the release heading.
- Add or update migrations when the schema changes.
- Confirm that every target profile has one coherent artifact role set and
  compatible panel, core-schema, broker, and component ranges.
- Generate `solovey-ui-release.json` with the immutable release artifacts.
  The canonical envelope binds the `main` or `beta` channel, monotonic
  sequence, validity window, restart/reboot/rollback classes, artifact names,
  roles, media types, sizes, SHA-256 digests, and provenance.
- Sign only in the release workflow with the configured Ed25519 private-key
  secret and key ID. Never put a private key in source, an image, a panel
  setting, logs, or release notes.
- Publish the same signed release set to the version tag and the matching
  `channel-main` or `channel-beta` pointer. The channel tag is a transport
  pointer, not a trust root; `solovey-ui-release.json` is the signed authority.
- Verify the public-root set embedded into the production build. An empty or
  unusable root set must remain fail-closed as `SIGNING_UNAVAILABLE`.
- Run `go test ./config ./database ./service ./internal/release`.
- Run the full validation gate before publishing artifacts.

## Complete release transaction

`scripts/release-targets.json` records the supported release inventory. Generic
Linux has full and core archives for amd64, arm64, armv7, armv6, armv5, 386 and
s390x, plus one component bundle. Every archive has an exact SHA-256 sidecar.
`solovey-ui-release.json` and its checksum bind the complete Linux update set.
Windows ships amd64 and arm64 ZIP packages with checksums; the operator-managed
Docker output covers amd64 and arm64. RISC-V is not a current release target.

`release.yml` coordinates the transaction. Its Windows reusable build never
publishes independently. Linux assembly and signature/complete-set verification
also run in preflight-only mode, retaining the candidate as an Actions artifact.
Publication verifies the immutable tag against the tested source before any
registry write. Release runs are serialized so channel updates have one writer.
After Windows and Linux are ready, the Docker owner builds both image targets;
only publication mode pushes their digests and complete multi-platform manifest.
The version Release is uploaded as a draft with both Linux and Windows files and
becomes public only after upload succeeds. The signed channel transport is
updated afterwards. A transport failure requires bounded recovery of the same
candidate, not substituting an older version or silently accepting partial assets.
Preflight defaults to no publication. A stored Actions candidate is qualification
evidence, never a substitute for an official release in the normal installer.

Use `node scripts/release-verify.mjs <linux-assets> <tag>` with the configured
public roots before publication and after downloading the remote Linux set.
Use `node scripts/release-verify.mjs <windows-assets> <tag> --windows` for the
Windows inventory/checksum gate. Production signing keys remain workflow-only.
Local signature fixtures demonstrate cryptographic behavior without establishing
production trust. Real candidates embed the configured public roots.

The Linux/component packagers normalize ownership, modes, ordering, timestamps
and gzip headers. Rebuilding identical inputs from independent roots must yield
identical archives and sidecars. A signed envelope additionally binds its chosen
sequence and validity window; signatures with different issuance inputs are not
required to match. Windows ZIP container metadata and OCI provenance may carry
creation metadata; compare their unpacked payloads and platform/config identity
when proving semantic reproducibility.

`scripts/release-source.mjs` records a new SHA-256 fingerprint over the canonical
LF source inventory (including installer, release scripts and workflows) and Git
file modes. Linux `BUILD_INFO.txt` records this fingerprint. Generated output,
private keys and temporary qualification files are outside that inventory.

The standalone native installer uses only its existing Bash/awk/base-system
prerequisites to validate GitHub JSON and read top-level string identities. JSON
layout is irrelevant; malformed/missing/duplicate identities and HTTP failures
fail closed. Archive checksums must authorize exactly the requested filename.
The initial HTTPS/checksum bootstrap remains distinct from the installed panel's
signed-manifest updater, whose embedded public roots authorize update manifests.

## Release trust and rotation

Production builds accept at most eight Ed25519 public roots. Each root has a
stable key ID, an `ACTIVE`, `NEXT`, or `RETIRED` state, a validity interval,
and an allowed release-sequence interval. `RETIRED` roots do not authorize new
releases. Test fixture keys never establish production trust.

A safe rotation is:

1. ship the new public root as `NEXT` while the old root remains `ACTIVE`;
2. verify that deployed versions observe the new root;
3. activate the new signer for a later sequence and ship the old root as
   bounded `RETIRED`;
4. remove the retired root only after its supported release and rollback
   horizon ends.

Never rotate by replacing the only active public root and signer in one step.
Never reuse or decrease a release sequence. Reissuing the same version still
requires a higher sequence and a newly signed envelope.

## Fetch and artifact policy

The panel fetches only a typed HTTPS source with a pinned origin, manifest
path, allowed redirect origins, and expected provenance. Response headers,
body size, read-idle time, redirect count, artifact count, artifact size, and
whole release-set size are bounded. A wrong channel, expired envelope,
unknown/retired signer, signature failure, rollback sequence, compatibility
miss, duplicate/missing role, provenance mismatch, or digest mismatch rejects
the entire release set.

Self-managed activation is performed only by the semantic privileged broker
and uses immutable release directories plus a managed current-release
reference. Operator-managed deployments, including Docker, do not grant the
panel staging, activation, rollback, host package-update, or reboot authority.
See the [deployment documentation in the README](../README.md) for supported
deployment profiles and operator entrypoints.

Broker-side update operation IDs are closed to the typed
`update-operation:` namespace and safe bounded characters. Artifact and
manifest identities are lowercase SHA-256 values. Semantic stage/apply/
rollback references bind the verb, typed operation ID and manifest digest, so
an exact replay is distinguishable from a conflicting operation.
