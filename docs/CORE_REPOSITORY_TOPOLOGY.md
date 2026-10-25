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

The Extended copy is a standalone repository because GitHub fork-network rules
do not permit a second ordinary fork in the existing Official fork network.
Shtorm's full Git history, original authorship, licenses and notices are retained.
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
The current Official module graph remains unchanged; its four accepted patches
are separately reproduced engine blockers, documented as temporary exceptions.

The old Shtorm-derived R7 branches in `MalenkiySolovey/sing-box` remain preserved
migration evidence. After the dedicated-copy pin passes Panel qualification,
they are historical refs without build authority. Do not delete or rewrite them
as part of this correction. The unfinished SSH/MTProxy Panel branch stays frozen
until the operator resumes R7 feature work.
