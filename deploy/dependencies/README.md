# Exact core source consumption

These JSON manifests record immutable build inputs and provenance. Engine source
corrections belong to linear Git commit queues in their own repositories.
Panel keeps no editable engine patch payload. Generated `git format-patch` files
may be used for contribution, review or source disclosure outside this source tree.

Provider authorities remain Official (`SagerNet/sing-box`) and Extended
(`shtorm-7/sing-box-extended`). Their copies are respectively
`MalenkiySolovey/sing-box` and `MalenkiySolovey/sing-box-extended`.
`sing-quic` has `SagerNet/sing-quic` authority. The existing AnyTLS library copy
has `anytls/sing-anytls` authority. Copies provide availability, reproducible
builds, development and temporary minimal integration queues.

Each manifest binds the authority/base/tree, copy repository, exact built
commit/tree, ordered Git series, source patch count, and Go module checksums.
Repository policy/CI commits are included in the series identity when compiled
from that head, but are excluded from the engine patch count. The identity is
SHA-256 of UTF-8 `solovey.core-patch-series/v1\n`, base SHA plus newline,
ordered full commit SHAs separated by newlines plus newline, and built tree SHA
plus newline. A commit SHA binds its original upstream tree.

Validate normal checkout metadata and print package build identity:

```sh
node scripts/ssm-integration-provenance.mjs check
node scripts/quic-integration-provenance.mjs check
node scripts/anytls-integration-provenance.mjs check
node scripts/extended-integration-provenance.mjs check
node scripts/extended-integration-provenance.mjs build-info
node scripts/core-source-provenance.mjs verify-remote
```

`verify-remote` fetches only Git objects into owned temporary directories,
checks upstream availability, pristine copy refs, exact trees, ancestry and
linear commit order, and removes those temporary Git objects. It checks all
consumed graphs without applying patches or editing module caches. For existing
local clones use each provenance command's `verify-git` mode.

Core copies own source correction tests and race CI. Panel CI owns source
consumption, provider identity, capabilities, actual runtime integration and
Official/Extended build regressions. SBOM/provenance records authority, base,
all patch commits and actual source tree. Corresponding-source archives contain
the authenticated source module bytes and original notices for both graphs.

Official remains the default, at its accepted 1.14.2 semantic baseline and
unchanged module/checksum pins. Extended's new copy pseudo-version begins
`v0.0.0` because that repository has no version tags; its runtime semantic
baseline remains Shtorm 1.14.3 / v1.14.3-extended-2.7.3. A Go pseudo-version is
source resolution metadata and does not create a product version or release.

See [repository topology](../../docs/CORE_REPOSITORY_TOPOLOGY.md) for the canonical
core-owned baseline, patch audit and update-procedure links.
