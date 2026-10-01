# Systemd installed runtime backing authority

The installer seals a full mount observation in the existing V1 runtime-root
contract. A later Systemd service has a different mount namespace and may run in
a later boot. Its startup binding therefore proves the same writable backing
directory, rather than equality of the install-time kernel presentation.

The Systemd adapter requires valid sealed persistent proofs, identical logical
and resolved targets, device major/minor, filesystem type and magic, source, and
the same path within the backing filesystem (`root + relative(mountpoint,target)`).
Both mount and statfs must be writable; superblock options must explicitly contain
`rw` and not `ro`. Missing or contradictory evidence fails closed.

Generic superblock flags (`sync`, `dirsync`, `mand`, `lazytime`), ACL/xattr mode
options and SELinux/Smack mount label options remain exact. Filesystem-specific
allocation, performance and presentation options are not backing identity. For
example, ext4 allocation scan tuning can be omitted on another boot. No device,
distribution or filesystem-specific exception selects this behavior.

Mount/parent IDs, namespace mountpoints and roots are coordinates, not persistent
identifiers. A bind mount may change all of them while exposing the same backing
directory. The resolved backing path comparison prevents substituting another
directory on the same device. Per-mount sandbox restrictions may differ from the
installer's host mount; live writability and owner-local path safety still apply.

This is deliberately conservative about device numbers and source names: their
equality is required, not asserted to be a universal filesystem UUID across
hardware reconfiguration. The V1 evidence cannot distinguish a privileged clone
with all the same observable identity fields. Root-controlled installation and
mount configuration remain inside the existing trust boundary.

After binding, the full current raw mount proof is retained separately. Recheck
compares its complete revision, including all options and namespace coordinates,
and verifies the retained installed runtime and application-owner generations.
An in-process remount or even a tuning change invalidates that retained authority.
Runtime ownership/mode and symlink checks remain with the managed-root owner.
Startup never rewrites or regenerates installed authority. Procd and Docker do
not use this Systemd projection; their existing exact semantics are unchanged.

References (semantics only; no transferred implementation):

- Linux v6.1 `fs/proc_namespace.c`, `show_mountinfo`, `show_sb_opts`:
  https://github.com/torvalds/linux/blob/v6.1/fs/proc_namespace.c
- Linux v6.1 `fs/ext4/super.c`, `ext4_show_options` and mount initialization;
  `fs/ext4/mballoc.c` allocation scan policy:
  https://github.com/torvalds/linux/blob/v6.1/fs/ext4/super.c
  https://github.com/torvalds/linux/blob/v6.1/fs/ext4/mballoc.c
- Kernel mountinfo interface:
  https://www.kernel.org/doc/html/v6.2/filesystems/proc.html
- Systemd's consumer of bind namespace semantics, `ReadWritePaths`:
  https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html

The captured beta.8 physical startup reason is `runtime_mount_binding_invalid`.
Its exact historical internal field was not captured. Reproduction isolates
`SuperOptions` equality with only `mb_optimize_scan=0` changed; that is reproduced
causal evidence, not retroactive physical tracing.
