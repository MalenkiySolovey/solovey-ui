# Native installer recovery contract

The standalone systemd installer owns its installation transaction. Before
changing an installation it records service/socket activity and exact persistent
and runtime enablement/alias/mask links. It stops socket activation and all
application/broker writers, then snapshots the release tree (including current),
configuration, hardened data, unit/profile files and CLI. It preserves the
original application tree on the same filesystem and mutates a copy, so rollback
also restores the device/inode identities bound by the broker client manifest.
The installation directory must be renameable within its parent; a mountpoint
that cannot be renamed is rejected before candidate mutation. Copy/staging
failure restores the preserved original rather than publishing partial files.
Explicit absence facts
distinguish a fresh path from a lost or incomplete backup.

An ordinary error, explicit exit, INT, TERM or HUP restores those files and links,
reloads systemd, and starts only units that were previously active. Activity and
enablement are checked after dependency activation. A previously inactive panel
remains inactive; rollback cannot create its DB by starting it. Paths absent
before installation are removed only when recorded as transaction-created.
Failed/nonactive units remain nonactive; historical systemd failure counters and
journal entries are not reverted. The installer does not roll back OS user/group
allocation or remove shared runtime directories.

The root-only `BACKUP_ROOT/.install-transaction` directory is a durable exclusion
and recovery fence. Snapshot and phase publication are synced before mutation.
An incomplete restore or uncatchable interruption (power loss/SIGKILL) leaves
this directory and its recovery material in place. Another installation refuses
to overwrite it. Preserve the directory, its recorded backup and current files
for bounded recovery; do not remove the fence and blindly retry. Failed restore
does not declare success or deliberately start the new deployment.

`--no-backup` controls retention after success. A rollback snapshot remains
mandatory. The default retains backups of previous installations and legacy
migrations; successful fresh installations discard their empty baseline.
Backups after a failed transaction are retained for diagnosis.

The manager's explicit `rollback` command is a separately requested restoration
of a selected historical deployment. The signed updater owns its own durable
activation/replay workflow. Neither is the failed-install recovery entry point.
OpenWrt APK/procd and FriendlyWrt storage lifecycles are unchanged.

Native owner identity files have exact contracts: deployment-profile and release
BUILD_INFO.txt are UID0/GID0/0644; instance-id is UID0/GID0/0400. Producers set
metadata before publication; the consumer reads Linux syscall stat metadata and
rejects unsafe inputs with path/expected/actual metadata, never file contents.
SSH proof retains UID0, the dedicated nonroot group and 2755. Generic root-owner
executable consumers still require root:root.

`tests/installer/native-ownership.sh` gates actual Linux ownership and complete
native manifest identity consumption plus real systemd rollback fixtures. The
fixtures prove host lifecycle semantics, not physical Debian/OpenWrt acceptance.
