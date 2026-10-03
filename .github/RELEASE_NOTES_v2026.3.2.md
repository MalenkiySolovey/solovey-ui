# Solovey UI 2026.3.2

This stable release improves deployment reliability, persistent storage and
privileged operations while preserving existing configuration and database
ownership.

- Native Linux deployments run the panel without root privileges or effective
  capabilities, with hardened systemd services and a constrained privileged
  broker. Cold socket activation, executable ownership, boot-time runtime
  directories and failed-install recovery are consistent across supported
  native deployments, including Debian 12, Debian 13 and Ubuntu 26.04.
- OpenWrt 25.12.5 provides package-managed full deployments for x86/64 and
  rockchip/armv8. Both panel and broker enforce kernel `no_new_privs` before
  process handoff. Persistent storage admission rejects unsupported or
  inconsistent backing and preserves the package manager's lifecycle ownership.
- FriendlyWrt 25.12.5 on NanoPi R76S uses the rockchip package with the supplied
  storage descriptor and an admitted persistent `/opt` mount. Firmware
  replacement, storage-media changes and other boards require their own
  supported deployment contract.
- Deployment Status and Doctor retain durable, bounded observations through
  restart and reboot. SQLite write contention and native sandbox mount binding
  no longer prevent broker-backed status and component registration.
- SSH management supports native OpenSSH and package-managed Dropbear through
  their existing owner-local service and recovery contracts. Signed updates
  retain fail-closed manifest, compatibility, sequence and artifact verification.
- Backup and restore preserve logical database and registered owner-file
  contracts, with integrity checks, restore rehearsal and durable audit records.
  All eight optional components retain their installation, data and lifecycle
  behavior, including Server Protection's capability-based enforcement and
  recovery.

Linux full/core archives, the component bundle, Windows amd64/arm64 packages,
Docker amd64/arm64 images, OpenWrt packages and the FriendlyWrt storage descriptor
are published through the complete release transaction. Exact SHA-256 sidecars
accompany downloadable assets; the signed release manifest authorizes the
generic Linux and component update set.
