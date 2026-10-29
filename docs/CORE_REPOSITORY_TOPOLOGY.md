# Core repository topology

Panel exposes two provider choices: Official and Shtorm Extended. Its existing
CoreRuntime, provider abstraction, owners, API, storage and Classic/Nexus
semantics remain shared. Provider selection stays in Host maintenance.

| Responsibility | Authority | Source copy | Pristine ref | Patch branch |
|---|---|---|---|---|
| Official engine | SagerNet/sing-box | MalenkiySolovey/sing-box | mirror/v1.14.2 | solovey/official-patches |
| Extended engine | shtorm-7/sing-box-extended | MalenkiySolovey/sing-box-extended | mirror/extended | solovey/extended-patches |
| Official QUIC dependency | SagerNet/sing-quic | MalenkiySolovey/sing-quic | mirror/official-6a3a24d | solovey/official-patches |
| Extended QUIC dependency | SagerNet/sing-quic | MalenkiySolovey/sing-quic | mirror/extended-75c3ac4 | solovey/extended-patches |
| Existing AnyTLS dependency | anytls/sing-anytls | MalenkiySolovey/sing-anytls | mirror/v0.0.11 | solovey/anytls-patches |

Each core checkout uses `origin` for the copy and `upstream` for its authority.
Pristine refs contain exact upstream commits and accept only reviewed
fast-forward synchronization. Development PRs target patch/topic branches.
Controlled queue updates use reviewed replay/rebase and `git range-diff`,
followed by core CI and Panel regressions before the build pin changes.

Repository ownership follows the module's actual source authority, not the
provider consuming it. Shared authority permits one Solovey dependency copy,
with independent consumer queues and immutable revisions when necessary.
Official and Extended therefore share `sing-quic` and `sing-anytls` repositories;
do not create `sing-quic-extended` or `sing-anytls-extended` for naming symmetry.
Each queue has its own explicit pristine base, linear history and qualification.
Selecting one queue must not inherit the other provider's patches or replacements.

Consume upstream directly while it works. Create or retain a Solovey dependency
copy only for an evidenced source change that belongs to that module. Fork the
actual selected authority, including a Shtorm authority where applicable.
When upstream gains an equivalent fix, prove equivalence, remove the local patch
and return to upstream where practical. A dependency's presence in the graph is
not a reason to create a copy. GitHub `fork` metadata does not define authority.

The current resolved graphs establish these cases; D records a required Solovey
source delta, with the underlying shared/different authority stated explicitly:

| Case | Module / actual authority | Consumers and ownership |
|---|---|---|
| D, different authorities (B) | SagerNet/sing-box; shtorm-7/sing-box-extended | Separate engine copies and queues above; four Official and fifty-six Extended source commits |
| C | SagerNet/sing-openconnect | Extended endpoint/DNS integration consumes upstream directly at `v0.1.1-0.20260929151225-cbd68c45de58` (pristine commit `cbd68c45de5883a695c844273ed040b0332fd017`); no Solovey source delta, fork or patch queue |
| D, shared authority (A) | SagerNet/sing-quic | One copy; one accepted Official source commit and two Extended source commits, independently pinned |
| D, shared authority (A) | anytls/sing-anytls | One copy; one accepted source commit selected by both providers |
| B | sing, sing-mux, sing-vmess, wireguard-go, tailscale | Official consumes SagerNet directly; Extended consumes the corresponding shtorm-7 repositories directly; no Solovey source delta |
| C | shtorm-7/dnscrypt/v2 | Extended-only replacement for ameshkov/dnscrypt/v2; no Solovey source delta |
| C | shtorm-7/mtg-multi | Extended-only replacement for dolonet/mtg-multi; no Solovey source delta |
| C | gobwas/ws | Extended Sudoku transport dependency; upstream v1.4.0 consumed directly, no Solovey source delta |
| C | kulikov0/headless-client | Extended Call HTTP/QUIC/WebRTC transport dependency; upstream v0.1.0 consumed directly, no Solovey source delta |
| C | shtorm-7/connect-ip-go | Extended-only replacement for Diniboy1123/connect-ip-go; no Solovey source delta |
| C | shtorm-7/go-cache/v2, shtorm-7/rmux, shtorm-7/workerpool | Extended-only selected authorities; no Solovey source delta |

These cases come from `go list -mod=readonly -m -json all`, separately using
`go.mod` and `go.extended.mod`; they describe the resolved source inventory,
including platform alternatives, rather than claiming every module is linked.
The two module files and sum files own exact selected dependency versions and
checksums. `deploy/dependencies/*-integration.json` owns full pristine/build SHAs,
trees, ordered source commits and queue identities for the Solovey copies.
Release provenance binds those records to the exact compiled revision. Recheck
lineage as a dependency becomes relevant; do not audit or fork unrelated
transitive dependencies in advance.

The Extended copy is a standalone repository because GitHub fork-network rules
do not permit a second ordinary fork in the existing Official fork network.
Shtorm's full Git history, original authorship, licenses and notices are retained.
Its baseline records authority URL, exact pristine SHA, selected upstream
version/branch, patch ancestry and update procedure independently of fork status.
No independent core project or third provider is introduced.

Canonical source documents and owned CI:

- [Official baseline and four retained proven blockers](https://github.com/MalenkiySolovey/sing-box/blob/solovey/official-patches/docs/solovey/OFFICIAL_COPY_BASELINE.md)
- [Extended baseline](https://github.com/MalenkiySolovey/sing-box-extended/blob/solovey/extended-patches/docs/solovey/EXTENDED_COPY_BASELINE.md), [delta audit](https://github.com/MalenkiySolovey/sing-box-extended/blob/solovey/extended-patches/docs/solovey/CURRENT_MIXED_FORK_DELTA_AUDIT.md), [logical series](https://github.com/MalenkiySolovey/sing-box-extended/blob/solovey/extended-patches/docs/solovey/EXTENDED_PATCH_SERIES.md)
- [Extended update procedure](https://github.com/MalenkiySolovey/sing-box-extended/blob/solovey/extended-patches/docs/solovey/UPSTREAM_REBASE_PROCEDURE.md) and [range-diff simulation](https://github.com/MalenkiySolovey/sing-box-extended/blob/solovey/extended-patches/docs/solovey/PATCH_SERIES_RANGE_DIFF.md)
- [QUIC source and patch audit](https://github.com/MalenkiySolovey/sing-quic/blob/solovey/extended-patches/docs/solovey/SING_QUIC_SOURCE_AND_PATCH_AUDIT.md), [Official QUIC baseline](https://github.com/MalenkiySolovey/sing-quic/blob/solovey/official-patches/docs/solovey/SING_QUIC_OFFICIAL_BASELINE.md)
- [AnyTLS existing correction](https://github.com/MalenkiySolovey/sing-anytls/blob/solovey/anytls-patches/docs/solovey/ANYTLS_COPY_BASELINE.md)

Panel owns the exact source/build metadata in `core/provider/pins.json` and
`deploy/dependencies/*-integration.json`. Actual changes and their tests belong
to the corresponding core Git queue. Detailed patch rationales stay there.
Each source patch retains a stable ID, exact authority/base, problem and module
ownership reason, regression evidence, upstream issue/PR or unsubmitted status,
and removal condition. Queue updates inspect conflicts and cleanly applied
patches for semantic obsolescence before focused dependency and Panel tests.
Panel may select, configure, verify, qualify, update and roll back a dependency
revision; it must not store that external module's canonical replayable source
patch payload. Migration removes duplicate payload only after Git equivalence,
owned dependency tests and immutable Panel consumption have been proven, while
retaining patch lineage and provenance metadata.

The existing consumption checks cheaply reject undeclared local source paths,
swapped engine authorities/copies, malformed or mutable pins, duplicate payload
metadata and inconsistent pristine ancestry, linear queues or build trees.
Core repositories qualify their source patches; Panel qualifies consumption.
Documentation-only policy updates do not require another release build or test
matrix. New providers do not justify copying their entire dependency graphs.
The current Official module graph remains unchanged; its four accepted patches
are separately reproduced engine blockers, documented as temporary exceptions.

The old Shtorm-derived R7 branches in `MalenkiySolovey/sing-box` remain preserved
migration evidence. After the dedicated-copy pin passes Panel qualification,
they are historical refs without build authority. Do not delete or rewrite them
as part of this correction. Reconcile unfinished product work with the selected
dedicated source queue before resuming it; never restore an obsolete mixed-copy
pin or a duplicate Panel source patch to continue a feature.
