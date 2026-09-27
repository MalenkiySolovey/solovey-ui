# Solovey UI v2026.3.3

This patch repairs two native Linux installation defects discovered during
Debian 13 recovery: owner-manifest validation rejected even correctly owned
identity files, and failed-install rollback could start a previously inactive
panel and create its database.

The native writer now consumes the correct Linux stat metadata. Producers
publish exact owner/group/mode before rename; rejection diagnostics identify
the affected path without revealing contents. The installer takes a quiescent
snapshot and restores previous data, service/socket activity and systemd
enablement on failure. An interrupted or incomplete rollback retains recovery
material and blocks an unsafe retry.

Native-hardened and native-legacy-root use the same repaired contract. Full,
core/minimal and component selection retain their existing boundaries. SSH
proof keeps UID0, the exact dedicated nonroot group and mode2755; unrelated
root-owner checks remain root:root. OpenWrt/FriendlyWrt lifecycles are unchanged.

Source/host qualification does not replace a new physical Debian recovery
acceptance run. Previously published releases are immutable.
