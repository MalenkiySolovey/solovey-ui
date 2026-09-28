# Solovey UI v2026.3.2-beta.3

This qualification prerelease repairs the generic native systemd sandbox that
prevented the embedded core from subscribing to Linux route updates. A clean
Debian 13 beta.2 installation completed successfully, but core startup reported
`subscribe route updates: address family not supported by protocol`.

The hardened main process now permits AF_NETLINK alongside AF_INET, AF_INET6
and AF_UNIX. It remains unprivileged with no Linux capabilities. Packaged units
and live deployment checks share one address-family policy. A real systemd
regression starts the pinned route/link monitor with zero capabilities,
reproduces the old failure, and verifies that broader families remain blocked.

This is a generic native Linux correction. Legacy-root compatibility and the
unavailable network-advanced profile retain their existing status. Full/core
composition, broker authority and OpenWrt/FriendlyWrt lifecycles are preserved.

Physical beta.2-to-beta.3 recovery and clean beta.3 acceptance remain pending.
Debian 12 and supported Ubuntu LTS require their own install/start/reboot smoke
tests before broad stable qualification. Stable/latest remains v2026.3.1;
use explicit `--version v2026.3.2-beta.3` for qualification.
