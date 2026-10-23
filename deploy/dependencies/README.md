# Pinned QUIC integration source

`sing-quic-integration.json` is the canonical exact dependency/source pin.
Go resolves the original sing-quic import path to one versioned public integration
module, authenticated by the two recorded Go module checksums. Official sing-box
v1.14.2 and its selected quic-go transport remain pinned. No source is modified in
a shared Go module cache.

`sing-quic-parent-control.patch` reproduces the complete eight-file integration
delta against the recorded official source commit. Original copyright and GPL
notices remain authoritative; the integration additions use GPL-3.0-or-later.
The fork's `SOLOVEY_INTEGRATION.md` describes each seam and its tests. This is
patched dependency source, not a byte-identical unmodified official runtime.

Validate a normal checkout and print package metadata:

    node scripts/quic-integration-provenance.mjs check
    node scripts/quic-integration-provenance.mjs build-info

Verify replay against both immutable commits already present in the small source
clone (read-only; only an owned temporary patch scratch tree is removed):

    node scripts/quic-integration-provenance.mjs replay /path/to/existing/sing-quic-clone

Runtime parent targets combine the protocol's original authenticated user with
the existing Solovey principal binding, exact inbound epoch, runtime generation
and service-local parent handle. Addresses never authorize a kick. Routed flows
remain in their existing trackers. The package build metadata explicitly names
the replacement commit, baseline, patch digest, checksums and license; Go build
information also retains the module replacement for SBOM consumers.

# Pinned SSM cache integration source

`sing-box-ssm-integration.json` records the additional exact official v1.14.2
source baseline and five-file `service/ssmapi` integration delta. The fork's
pseudo-version is Go module resolution metadata; it does not repin the semantic
baseline to an official v1.14.3 release. Protocols and dependency requirements
are unchanged; newer compatible Solovey security dependencies remain selected.

`sing-box-ssm-cache.patch` is the complete GPL-3.0-or-later delta. Test with
`node scripts/ssm-integration-provenance.mjs check`, print package metadata with
`build-info` or `manifest`, and replay with
`replay /path/to/existing/sing-box-clone`. Both commits must be present; only the
small verified tool-owned replay scratch is removed.

SSM remains the authentication/counter owner. Solovey injects private storage
rooted in the deployment data directory's `ssm` folder. Existing unowned,
indirect, public or corrupt state is rejected with redacted manual-required
diagnostics and retained for correction. Absent `cache_path` remains without
persistence. Private logical backups preserve explicit present/absent state;
immutable restore seeds have separate working state. Backups capture the last
persisted SSM snapshot, not an instantaneous synchronized panel/SSM snapshot.
Configured cache state requires the existing `OWNER_FILES_V1` deployment backup
capability; export/restore cannot silently omit it when that capability is absent.
The official one-minute periodic save remains; successful Close flushes after
the periodic writer drains with one two-second budget.

Linux/Windows package build metadata and Docker's `SSM_INTEGRATION.json` identify
the replacement, patch, checksums and license. Standard Go build information
retains both module replacements for SBOM consumers. Patched source is not
byte-identical unmodified official core.

R76S physical qualification found that normal SSM replacement could fail solely
because HTTP shutdown had already closed its served listener, forcing a full
core restart. The pinned follow-up accepts that expected listener net.ErrClosed
at the existing SSM owner. Real served-listener/port-reuse/concurrent-close and
CoreRuntime hot-replacement regressions retain the existing generation, private
API and StatsTracker. TLS/cache errors continue to be returned.

# Explicit Shtorm Extended integration source

`sing-box-extended-integration.json` records the separate native Extended engine
and QUIC graph selected by `go.extended.mod`. The semantic baseline is Shtorm
v1.14.3-extended-2.7.3 at `d9442d14b19ddd02605bf3ee893a171947038e19`.
Its maintained engine pseudo-version is module metadata; the runtime semantic
version remains 1.14.3. The default Official graph and its notices above stay
authoritative for Official builds.

The two Extended patches are cumulative, complete deltas against the recorded
native engine and QUIC baselines. They retain native dependency replacements,
original licenses and Scope lifecycle ownership. Bounded integrations preserve
Solovey's existing SSM storage, WebSocket handling, WireGuard IPC/AWG seam,
Naive HTTP/3 and authenticated QUIC parent lifecycle, hot object mutation and
private API shutdown. The fork notices describe the specific source gaps and
regression tests. They introduce no alternate Panel generation, flow tracker,
authentication, subscription or storage owner.

    node scripts/extended-integration-provenance.mjs check
    node scripts/extended-integration-provenance.mjs build-info
    node scripts/extended-integration-provenance.mjs replay /existing/engine/clone /existing/quic/clone /owned/scratch/parent

Replay reads immutable source objects from the existing clones, creates one
small scratch tree under the explicitly supplied parent, compares every patched
file to its pinned commit, and removes only that verified scratch tree. It never
modifies a clone or module cache. Source provenance and local/CI lifecycle proofs
do not assert full semantic/UI parity, artifact trust or device qualification.
