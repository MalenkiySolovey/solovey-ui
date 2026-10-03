# Solovey UI v2026.3.2-beta.3

This prerelease permits the native hardened panel's embedded core to subscribe
to Linux route and link updates. Its systemd address-family policy includes
AF_NETLINK alongside AF_INET, AF_INET6 and AF_UNIX. The panel remains
unprivileged with no effective capabilities; broader families remain blocked.

Packaged units and deployment checks share the same policy. Full/core
composition, broker authority, legacy-root compatibility and package-managed
deployment lifecycles retain their existing contracts.

Select this historical prerelease explicitly with
`--version v2026.3.2-beta.3`. Default installation resolves the current stable
release.
