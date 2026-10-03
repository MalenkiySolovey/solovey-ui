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

Use a dedicated branch and reviewed PR for release changes. The accepted main
commit is the immutable release source. A new release uses a fresh monotonic
sequence and a complete verified artifact set. Never overwrite a public release,
move its tag, reuse retired receipts, or substitute a stored candidate for the
official installer release.

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
