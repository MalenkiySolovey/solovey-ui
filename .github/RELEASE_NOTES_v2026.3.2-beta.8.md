# Solovey UI 2026.3.2-beta.8

Native installation now registers its existing tmpfiles policy for every boot.
Previously the installer applied the policy only during installation/update;
after a reboot, systemd could recreate the socket parent as root:root 0750.
The nonroot panel then received EACCES before connecting, leaving the broker
inactive and Deployment Status unavailable despite listening socket units.

The registration points to the single packaged policy. Installer and management
backups preserve it, failed installation and rollback restore its prior state,
and uninstall removes it. Parent and socket permissions remain 0750 and 0660;
the main panel retains zero capabilities and the broker's authentication,
pidfd, executable and cgroup checks are unchanged. CAP_KILL remains absent.

The native regression installs through the production owner, discards volatile
runtime state, discovers the boot policy, and checks the first two authenticated
Status calls with an initially inactive broker. Existing Doctor/SQLite durability
and concurrent writer coverage remains in place.

The mechanism is reproduced on a Linux host with real systemd; the exact
internal cause in the historical physical beta.7 capture remains unknown.
Physical beta.7-to-beta.8 qualification requires a separate run. Stable/latest
remains v2026.3.1.
