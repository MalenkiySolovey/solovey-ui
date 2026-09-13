# OpenWrt database persistence admission

V3 is the canonical deployment-owned proof. It proves that the fixed database
folder is on admitted persistent storage; it does not prove an APK transaction
committed, hardware power-loss safety, or a wall-clock bound on kernel I/O.

## One fact, one owner

The canonical platform provenance owner is the workspace OpenWrt reference lock
(A1/A2) and its pinned OpenWrt fstools recipe. Build target profiles select that
release. `scripts/openwrt-persistence-authority.mjs` generates the typed owner-local
`persistence_authority_generated.go` projection and verifies the lock, local HEADs,
recipe and release profiles. Stage construction checks projection drift before
source materialization. Go qualification independently checks the same chain.
The generated file is included in the shipping source fingerprint.

The runtime consumes this deployment-selected identity. It never looks up a
version, board, distribution, upstream checkout or global platform manager.
`deploy/openwrt` owns persistence admission; policy-free `mountevidence` owns
current Linux mount observations. Storage owners retain file/database write
and publication barriers. The APK owner retains its own transaction mechanics.

## Explicit proof classes

- `DIRECT_PERSISTENT_MOUNT`: writable mount with an approved filesystem from
  the existing shared OpenWrt persistence policy; no overlay fields.
- `PINNED_FSTOOLS_OVERLAY`: writable overlay root, exact fstools persistent
  source and upper/work labels, selected typed platform authority, and a current
  writable direct `/overlay` mount in the approved filesystem family. Visible
  upper/work labels must resolve without redirection to that same mount. Opaque
  labels are permitted because mount-time paths need not remain reachable.

Root and backing mount revisions are fenced across observation; every consumer
reobserves the topology and verifies root/backing continuity plus capacity.
V3 validation checks the same semantics as production. Missing/unknown classes,
legacy V2 proofs, volatile/read-only/unsupported backing, absent authority,
contradictory or nested mount evidence and drift fail closed. Preparation writes
a fresh V3 proof before starting services, replacing old persisted V2 metadata.

Unknown noncanonical overlay topology now fails closed without trying physical
FIEMAP/writeback. A physical mapping alone is neither a platform contract nor a
bounded persistence decision. No package/service consumer can inject or call
`ProbeOverlayBacking`; no substitute timeout, worker, retry or background helper
is used. The neutral historical probe remains unused by production callers.

## Physical defect and evidence boundary

r18 unconditionally created a probe file, file-synced it, requested FIEMAP and
called `SYS_SYNCFS` before consuming fstools persistence. Validation and recheck
also required the probe. Physical F2FS entered uninterruptible checkpoint I/O in
post-upgrade and prevented APK registration from committing. The earlier claim
that this probing path was bounded was incorrect.

V3 removes that unnecessary whole-filesystem operation from Solovey admission.
Normal file/database fsync and APK's own post-registration sync remain. Their
physical execution and full recovery are separately qualified on OpenWrt; a
userspace timeout cannot guarantee kernel filesystem progress. Source/host gates
must not be represented as a physical F2FS recovery PASS.

Remediation evidence: workspace `.artifacts/r18-package-durability-remediation/`.
Final physical V3 recovery and subsequent exact r21 fresh-root installation are
accepted in the [canonical support baseline](../../../../Разное/OS/CROSS_PLATFORM_ARCHITECTURE.md#8-openwrt-support-baseline).
The historical source-only qualification above is not the final support status.
