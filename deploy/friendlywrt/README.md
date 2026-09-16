# FriendlyWrt deployment storage contract

FriendlyWrt consumes the same OpenWrt APK with an explicit deployment storage
selection. The template is `deploy/friendlywrt/deployment-storage.json`.
Deployment setup supplies it as `/etc/solovey-ui/deployment-storage.json`, owned
by root:root with mode 0444, inside the root-owned 0711 package configuration
directory. The package does not choose a platform from a distribution name.

This template selects `/opt/solovey-ui` as the durable root and requires a
`DIRECT_PERSISTENT_MOUNT` at `/opt`. FriendlyELEC owns partition creation,
resize and fstab mounting. Solovey does not mount, repartition, expose `/data`
or modify initramfs. A missing, read-only, indirect, stacked, ambiguous or
changed mount fails admission. Mountinfo, statfs, statx mount ID, target device,
block-device source and sysfs identity must agree. No hidden overlay fallback
is supported.

Before deploying, qualify the actual visible mount and its current block/UUID
identity against the boot medium and the exact image. A bounded file canary
must survive a normal reboot on that same filesystem, then be removed. A
successful package simulation is also required before the first installation.

The selected root contains the DB child and the deployment-owned stable
instance UUID. SQLite, secret settings, application/component state and
transaction recovery facts use the injected DB folder. Package executables,
libraries, frontend, init scripts and composition metadata keep their existing
paths. Runtime sockets, pids and Server Protection recovery objects retain
their `/run` owner. Reconstructable caches and temporary export/rehearsal work
use separate deployment-injected `/tmp/solovey-ui` paths. SQLite transaction
staging/fallback files remain beside the DB where atomic recovery requires it.

Without a selection, the existing stock OpenWrt DB path and pinned fstools
persistence proof remain in force. Generic Linux retains its existing defaults.

## Explicit logical backup and restore

Selected external storage enables the owner-file extension of the normal
Solovey logical backup. It contains the existing logical table snapshot plus
bounded, hashed file contributions from semantic owners. Certificate settings
and TLS path fields contribute their referenced PEM files; fallback-html
contributes asset and publish bytes, including when the component is disabled.
Deployment identity is recorded as source provenance. An unavailable owner,
missing required file, digest mismatch or unsupported destination capability
fails closed. The extension is not enabled for the accepted stock deployment.

Use Solovey's encrypted backup flow for stored or transported backups. The
inner logical artifact contains sensitive material; existing secret-setting
encryption and the protected backup codec retain their owners. Temporary
artifacts and restored private files use restrictive permissions. There is no
raw live SQLite backup authority and no WAL/SHM dependency.

Restore first rehearses on a private database without publishing live files.
Each file owner validates its semantic keys and selects its own destination.
Execution publishes immutable content-addressed files and updates its database
references within the existing rollback-protected restore lifecycle. Failure
reopens the old database and preserves the files it references. No archive
path grants arbitrary write authority. Published fallback sites remain
deactivated until their existing owner verifies a new publish.

The stable UUID belongs to root deployment setup. Restoring application state
on the same deployment preserves that UUID. A fresh deployment creates a new
UUID; a backup records the old source identity without impersonating it or
overwriting the destination's root-owned process authority. Runtime manifests
and firewall/live process observations are rebuilt, not transplanted.

Files under `/opt` are not assumed to be covered by OpenWrt `/etc` archives.
Native sysupgrade backup/restore and firmware preservation are rejected for a
selected external-storage deployment. FriendlyWrt firmware/image replacement
is **not yet qualified**. A protected Solovey logical backup is required before
any separately authorized replacement; whether a particular FriendlyELEC
replacement preserves `/opt` must be proven in that later qualification.
