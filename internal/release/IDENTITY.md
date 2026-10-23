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
Compatibility and installation policy remain with their owners. The accepted
release and trust roots retain their existing identity.

Verified values are request-scoped read-only snapshots. Concurrent readers can
resolve independently; exported slices must not be mutated concurrently. Any
edited metadata is rejected by the signed canonical binding on subsequent use.

## Provider-bearing release metadata

`solovey.release/v2` declares one or both core providers using the source
owner's exact source projection. Each Panel artifact references its declared
provider ID. Every declared provider must have artifacts, and every target it
declares must contain both full/core profiles. Duplicate names, conflicting
roles, undeclared providers and provider claims on component artifacts fail
validation. The existing canonical and Ed25519 owners cover every source field.
Envelope and manifest schemas must agree. Duplicate decoded JSON member names,
invalid UTF-8, trailing documents and unknown critical fields fail closed.

V2 selection requires `Verified.PanelArtifactsForProvider` with an explicit
provider, target and profile. The legacy selector refuses v2. V1 cannot declare
providers or select Extended, and never fabricates exact source facts. The v1
canonical and signed-envelope bytes remain unchanged, with fixed pre-change
digest fixtures. Artifact identity projects the provider source as an immutable
value; repeated projections retain value equality. Its zero value is omitted
from legacy JSON.

Source declarations can describe a future immutable module version. Matching
one does not place it in the running process's compiled qualification catalog.
`core/provider.ModuleSourceMatches` compares only observed module facts: the
original engine semantic requirement and its single exact replacement
path/version/checksum. The signed publisher supplies source/upstream commit
provenance; a binary's module tuple alone cannot prove those commit labels.
The capability owner uses `core/provider.ResolveModule` for its already
projected tuple, then checks its actual original requirement separately. It
does not synthesize incomplete Go build metadata for the stricter reader.

## Offline executable observation

`PanelArchiveReader` owns the shared bounded tar/gzip grammar used by broker
extraction and the local offline observer. It accepts canonical root/systemd
directory records and safe regular members, and rejects duplicate labels,
links, traversal, excessive entries/bytes, missing tar end records and invalid
gzip trailers or nonzero trailing payloads. Consumers must reach EOF before
trusting a complete inventory; it grants no filesystem or installation right.

The local command also accepts `--archive CANDIDATE` and emits
`solovey.panel-archive-observation/v1`. It hashes the complete archive and each
member, spools only the bounded Panel executable to an exclusive private file,
then observes that executable without running it. Text build metadata and
archive filenames cannot select or override its provider. The temporary file
is removed on success and failure. This is packaging observation, not a
complete package/SBOM proof or host execution authorization.

`InspectPanelBuild(io.ReaderAt, source)` observes Go build metadata and the ELF
or PE executable container without executing code. It checks the first-party
module, unique pinned linked dependencies, provider ABI tag, full/core profile,
OS, CPU class/endianness and architecture settings. ARM levels map to the
declared armv5/armv6/armv7 archive target. Architecture-level observation is not
a declaration that an installed device supports that instruction set.

`Verified.InspectPanelArtifact` additionally requires the private signature
binding, exact artifact membership and matching signed source/target/profile.
Its caller still verifies archive/member digests and owns stable opened-object
checks. Host execution must retain the existing `executableobject` mechanism
and revalidation; this read-only inspector creates no execution authority.
The linked-module digest covers observed dependency metadata, not the complete
resolved graph, license inventory or SBOM.

The local packaging command `solovey-artifact-info BUILT_PANEL` reads a bounded
strict source declaration on stdin and emits source/target/profile/module
observation plus the file's SHA-256. It accepts only a bounded regular file,
checks the opened object against its initial stat, and checks stability after
hashing. It never starts the candidate, opens a Panel database or installs it.
Its path is explicit operator input, never a Panel or management API argument.

Current release production, installer and Update/broker maintenance admission
still use their existing legacy contracts. They do not consume v2 or perform
provider transitions. Packaging integration, checkpoint/configuration binding,
maintenance activation, health verification and rollback remain separate work.
The binary qualification harness uses ephemeral fixture signing keys and checks
actual full/core executables; it publishes no release artifact.
