# Signed artifact identity

`Verified.ArtifactIdentity` derives typed metadata only from an unchanged result
of the existing Ed25519 `Verify`. Constructing the public metadata fields or
providing a SHA alone cannot create the private signature binding. Projection
is not a transferable authorization token: update admission still fetches and
verifies the current pinned manifest, channel, sequence, validity and compatibility.

Artifact ID binds kind and content digest. Identity contains owner, signed
version/target, size, signer, provenance and signed-manifest revision. It contains
no URL, cache path or transport attempt. A pinned mirror can transport the same
bytes without becoming a publisher. `Fetcher` retains its origin, redirect,
timeout, read-idle, byte and digest checks; no universal downloader is added.

The real panel consumer selects through `Verified.PanelArtifacts`; broker
staging requires exact signed artifact, operation manifest, version and target
before opening its cache. The component consumer checks current compiled/installed
inventory against `Verified.ComponentArtifacts` during update admission. Component
entries resolve to the signed component-catalog class, matching release tooling's
existing binding, and retain their own version/schema ranges. A panel archive
cannot stand in for the catalog. Check responses expose both typed projections.

Component installation remains compiled inventory activation. There is no
separate component bundle downloader to migrate. This change creates none.
Compatibility and installation policy remain with their owners. No schema,
trust root, release format or current accepted release is changed.

Verified values are request-scoped read-only snapshots. Concurrent readers can
resolve independently; exported slices must not be mutated concurrently. Any
edited metadata is rejected by the signed canonical binding on subsequent use.
