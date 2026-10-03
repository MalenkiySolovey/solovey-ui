# Solovey UI 2026.3.2-beta.8

Native installation registers the packaged tmpfiles policy for every boot.
The nonroot panel can connect to the broker after volatile runtime state is
recreated. Socket-parent and socket modes remain 0750 and 0660.

Installation backups, rollback and uninstall include this boot registration.
Panel capabilities and broker credential, pidfd, executable and cgroup checks
retain their existing contracts. CAP_KILL remains absent.

Select this historical prerelease explicitly with
`--version v2026.3.2-beta.8`. Default installation resolves the current stable
release.
