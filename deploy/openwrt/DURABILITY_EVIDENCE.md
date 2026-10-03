# OpenWrt database persistence contract

Deployment owns admission of the database directory to persistent storage.
The canonical V3 proof binds the selected storage identity and current mount
evidence. It does not assert a completed package transaction, hardware power-loss
safety, or a wall-clock bound on kernel I/O.

## Ownership

The pinned OpenWrt source lock and fstools recipe select the platform authority.
`scripts/openwrt-persistence-authority.mjs` projects it into the deployment-owned
generated Go contract. Package staging checks source/projection drift before
materialization; the generated contract participates in the source fingerprint.

Runtime admission consumes that injected authority. It does not inspect an
upstream checkout, detect a distribution, or select a global platform manager.
`deploy/openwrt` owns admission; policy-free `internal/ops/mountevidence` owns
current Linux mount observations. Database and file owners retain their own
write/publication barriers. The package manager owns package transactions.

## Supported storage classes

- `DIRECT_PERSISTENT_MOUNT`: a writable direct mount using an approved persistent
  filesystem, without overlay proof fields.
- `PINNED_FSTOOLS_OVERLAY`: a writable root overlay with the exact selected
  fstools source and upper/work labels, backed by a current writable direct
  `/overlay` mount in the approved filesystem family. Visible labels must resolve
  without redirection to that same backing mount. Opaque mount-time labels are
  admitted only through the pinned platform contract.

The shared persistent filesystem policy admits ext2, ext3, ext4, f2fs, ubifs,
jffs2, btrfs and xfs. Admission also requires sufficient capacity and consistent
mount topology; the filesystem name alone does not authorize persistence.
FriendlyWrt's selected direct `/opt` mode additionally uses its deployment-owned
[storage descriptor](../friendlywrt/README.md).

## Fail-closed admission

Root and backing revisions are fenced across observation. Every consumer
reobserves topology and checks continuity and capacity. Missing authority,
unknown proof classes, legacy V2 proofs, volatile/read-only/unsupported backing,
contradictory or nested mount evidence, path redirection and generation drift
are rejected. Preparation publishes a fresh V3 proof before service launch.

Admission never substitutes physical block mapping for platform authority and
does not issue FIEMAP or whole-filesystem sync operations to infer overlay
durability. Normal file/database fsync and the package manager's own transaction
barriers remain their respective owners' responsibility. Userspace timeouts
cannot guarantee progress of kernel filesystem I/O.

Changing the backing filesystem or mount topology requires validating the new
deployment storage contract. A successful source build does not establish
hardware durability or firmware-replacement preservation.
