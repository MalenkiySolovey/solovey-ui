# Release policy

The canonical [release contract](../.github/RELEASE_CONTRACT.md) owns version,
artifact inventory, signing, immutable publication and channel transport rules.
`config/identity/version` is the application version authority;
`scripts/release-targets.json` owns the current target inventory.

## Channels and promotion

Stable releases use the `main` signed channel and ordinary year-based SemVer.
Prereleases use `YYYY.RELEASE.PATCH-beta.N`, are published with
`prerelease=true` and `latest=false`, and use the `beta` signed/registry channel.
Select prereleases by explicit version; GitHub's latest-release endpoint resolves
stable releases.

`scripts/release-train.json` and the preflight validator guard the reviewed
release channel. Promotion requires the supported platform/storage contracts to
remain accepted and every blocking candidate gate to pass. Source and host tests
protect these contracts; they do not expand the supported hardware matrix.
The operator-approved 2026.5.1 corrective release follows the published
2026.5.0 and repairs ordinary signed-v2 updater admission. Its reviewed train
records `acceptedPrerelease=null` and no pending acceptance; publication still
requires exact-source CI and candidate/public acceptance. No subsequent version
is reserved. Historical 2026.5.0 publication and its updater blocker remain
recorded independently.

Use a dedicated branch and reviewed PR for release changes. The accepted main
commit is the immutable release source. A new release uses a fresh monotonic
sequence and a complete verified artifact set. Never overwrite a public release,
move its tag, reuse retired receipts, or substitute a stored candidate for the
official installer release.

## Main channel renewal

The daily Audit Dashboard fails the signed-channel check when the public main
envelope is invalid, expired, or has at most three days left. The audit uses
only the public trust roots and never signs or publishes.

From the canonical Product repository, use the existing release workflow on
`main` with the exact latest stable tag (example for the current stable):

```sh
gh workflow run release.yml --ref main -f tag=v2026.5.1 -f renew_main=true -f preflight_only=false
```

This mode authenticates the live channel and the immutable version envelope,
checks the signed artifact identities against both releases, and reissues the
same payload with a higher sequence and a fresh seven-day validity window. It
uses the existing signer and release concurrency group. It replaces only
`solovey-ui-release.json` and its checksum on `channel-main`, verifies the two
public downloads, and logs the sequence, expiry, and envelope SHA-256. It
cannot build or publish a version, change beta, or rotate a signer.

If a transport error interrupts the upload, check the live asset and the
workflow's retained verified renewal bytes before any retry. Never restore a
lower sequence. Missing or ambiguous channel state requires the release owner
to reconcile the interrupted publication; do not bypass the verifier.

## Package versions

The OpenWrt package metadata owner encodes a prerelease such as
`2026.3.2-beta.N` as `2026.3.2_betaN-r25`; the suffix is the package revision.
Runtime versions, BUILD_INFO, Git tags and signed manifests retain SemVer.
Numeric beta ordering sorts below the corresponding stable version.
FriendlyWrt's storage descriptor is version independent.

Database version history never downgrades. Installing an older prerelease does
not relabel a database already migrated by a newer stable release.

## Candidate validation

The canonical workflow defaults to preflight mode. It builds and verifies
Linux full/core archives, component packages, Windows packages, Docker targets
and OpenWrt packages before publication. The complete transaction and independent
download verification are specified in the canonical release contract.
Production private signing keys remain workflow-only.
