# OpenWrt & FriendlyWrt Upstream Reference Lock

This document provides the authoritative reference inventory and key file map for OpenWrt, FriendlyWrt, and canonical proxy service integration patterns. It is intended for inspection and cross-referencing before designing or implementing any OpenWrt platform mechanisms in Solovey UI.

---

## 1. Upstream Repository Inventory

### Group A: Platform Authorities (OpenWrt Canonical)

#### A1. OpenWrt 25.12.5 (Core Base System)
- **Authority Level**: `PLATFORM_CANONICAL`
- **Directory**: `upstreams/openwrt-openwrt-25.12.5`
- **Remote URL**: `https://git.openwrt.org/openwrt/openwrt.git`
- **Requested Ref**: `v25.12.5`
- **Actual Ref**: Tag `v25.12.5` (Detached HEAD)
- **HEAD SHA**: `f0a60eee2fe051741c643ea6118718aae1ef17fb`
- **Latest Commit Date**: `2026-06-30 00:03:42 +0200`
- **Latest Commit Title**: `OpenWrt v25.12.5: adjust config defaults (Hauke Mehrtens)`
- **Git Status**: Clean
- **License**: GPL-2.0
- **Purpose**: Canonical authority for package lifecycle, apk integration, procd packaging, rc.common, base-files, sysupgrade, fw4 integration boundaries, fstools package pin, and OpenWrt 25.12.5 platform behavior.

#### A2. fstools — Pinned to OpenWrt 25.12.5
- **Authority Level**: `PLATFORM_CANONICAL`
- **Directory**: `upstreams/openwrt-fstools-25.12.5-pinned`
- **Remote URL**: `https://git.openwrt.org/project/fstools.git`
- **Source Pin Definition**: `openwrt-openwrt-25.12.5/package/system/fstools/Makefile` (`PKG_SOURCE_VERSION:=16718b6e3c0fc7db7be6ae5848db0eae88ac8a8b`)
- **Requested Ref**: `16718b6e3c0fc7db7be6ae5848db0eae88ac8a8b`
- **Actual Ref**: Commit `16718b6e3c0fc7db7be6ae5848db0eae88ac8a8b` (Detached HEAD)
- **HEAD SHA**: `16718b6e3c0fc7db7be6ae5848db0eae88ac8a8b`
- **Latest Commit Date**: `2026-05-23 17:44:55 +0200`
- **Latest Commit Title**: `libfstools: mount f2fs overlay with zstd compression (Robert Marko)`
- **Git Status**: Clean
- **License**: GPL-2.0
- **Purpose**: Primary semantic authority for rootfs_data, overlay root, ramoverlay, extroot, mount_root, persistent/volatile writable root behavior, JFFS2, UBIFS, F2FS, EXT4, and overlay lifecycle.

#### A3. OpenWrt Official Packages (Branch 25.12)
- **Authority Level**: `PLATFORM_CANONICAL`
- **Directory**: `upstreams/openwrt-packages-25.12`
- **Remote URL**: `https://github.com/openwrt/packages.git`
- **Requested Ref**: `openwrt-25.12`
- **Actual Ref**: Branch `openwrt-25.12`
- **HEAD SHA**: `a970210a468e7effe44f50401e6595f82cc21919`
- **Latest Commit Date**: `2026-08-27 14:00:33 +0200`
- **Latest Commit Title**: `adblock: replace the CARTO basemap with local outlines (Dirk Brenken)`
- **Git Status**: Clean
- **License**: Various open source licenses per feed package
- **Primary Reference**: `net/sing-box`
- **Purpose**: Official OpenWrt packaging for sing-box, service definitions, USERID/group declarations, dependency contracts, default configurations, and init scripts.

---

### Group B: OEM Authorities (FriendlyWrt / FriendlyELEC)

#### B1. FriendlyWrt Source Tree (Fork 25.12)
- **Authority Level**: `OEM_CANONICAL`
- **Directory**: `upstreams/friendlyarm-friendlywrt-25.12`
- **Remote URL**: `https://github.com/friendlyarm/friendlywrt.git`
- **Requested Ref**: `master-v25.12`
- **Actual Ref**: Branch `master-v25.12`
- **HEAD SHA**: `27d9b25f21b94b7f812836449769f026bb280c58`
- **Latest Commit Date**: `2026-07-22 19:45:22 +0800`
- **Latest Commit Title**: `rockchip: update base-files for nanopi-r28s (Lawrence-Tang)`
- **Git Status**: Intact; colon filenames in upstream `package/utils/usb-modeswitch-official/files/usb_modeswitch.d` excluded via sparse-checkout due to Windows NTFS constraints.
- **License**: GPL-2.0 / OEM additions
- **Purpose**: Canonical authority for FriendlyELEC-specific modifications, RK3576/R76S base-files, rootfs/overlay customization, `root/setup.sh`, hardware fan control, and platform sysupgrade behavior.

#### B2. FriendlyWrt Manifests (25.12)
- **Authority Level**: `OEM_CANONICAL`
- **Directory**: `upstreams/friendlyarm-friendlywrt-manifests-25.12`
- **Remote URL**: `https://github.com/friendlyarm/friendlywrt_manifests.git`
- **Requested Ref**: `master-v25.12`
- **Actual Ref**: Branch `master-v25.12`
- **HEAD SHA**: `f2cd2723d13afec08a14c46213aa64e484fbebbf`
- **Latest Commit Date**: `2026-04-25 15:36:27 +0800`
- **Latest Commit Title**: `bump to openwrt-25.12 (Lawrence-Tang)`
- **Git Status**: Clean
- **License**: MIT
- **Purpose**: Defines exact component repositories and revisions used in FriendlyWrt RK3576 builds.

##### Parsed RK3576 Manifest (`rk3576.xml`):
- **Kernel**: `kernel-rockchip` @ `refs/heads/nanopi6-v6.1.y`
- **U-Boot**: `uboot-rockchip` @ `refs/heads/nanopi5-v2017.09`
- **RKBIN**: `rkbin` @ `refs/heads/nanopi_m5`
- **SD Fuse**: `sd-fuse_rk3576` @ `refs/heads/kernel-6.1.y` (linked to `out`)
- **Device Common**: `friendlywrt_device_common` @ `refs/heads/master`
- **Device RK3576**: `friendlywrt_device_rk3576` @ `refs/heads/master-v25.12`
- **Configs**: `friendlywrt_configs` @ `refs/heads/master-v25.12`
- **Scripts**: `friendlywrt_scripts` (linked to `build.sh`)
- **FriendlyWrt Source**: `friendlywrt` @ `refs/heads/master-v25.12`
- **Toolchain**: `prebuilts` @ `refs/heads/master`

---

### Group C: Working OpenWrt Proxy Applications (Primary References)

#### C1. Momo (OpenWrt Momo / sing-box)
- **Authority Level**: `PRIMARY_APPLICATION_REFERENCE`
- **Directory**: `upstreams/nikkinikki-OpenWrt-momo`
- **Remote URL**: `https://github.com/nikkinikki-org/OpenWrt-momo.git`
- **Requested Ref**: `main`
- **Actual Ref**: Branch `main`
- **HEAD SHA**: `72f5c46b5b65ad95f8f786f024c98204e47cd3dd`
- **Latest Tag**: `v1.2.1`
- **Latest Commit Date**: `2026-08-17 15:31:32 +0800`
- **Latest Commit Title**: `fix: redundant cron task in edge cases (Joseph Mory)`
- **Git Status**: Clean
- **License**: GPL-3.0
- **Purpose**: Modern reference for sing-box + procd + Firewall4 (nftables) + ucode integration, apk/opkg dual-packaging, runtime/persistent state management, and cron lifecycle.

#### C2. HomeProxy (ImmortalWrt HomeProxy)
- **Authority Level**: `PRIMARY_APPLICATION_REFERENCE`
- **Directory**: `upstreams/immortalwrt-homeproxy`
- **Remote URL**: `https://github.com/immortalwrt/homeproxy.git`
- **Requested Ref**: `master`
- **Actual Ref**: Branch `master`
- **HEAD SHA**: `edece28a0085f36d469ec82c8d45f562f602db53`
- **Latest Commit Date**: `2026-08-11 14:38:48 +0800`
- **Latest Commit Title**: `chore(resources): update geodata (Tianling Shen)`
- **Git Status**: Clean
- **License**: GPL-3.0
- **Purpose**: Canonical implementation of sing-box running under procd with comprehensive Firewall4 nftables hooks, ucode config generation (`homeproxy.uc`, `firewall_pre.uc`), runtime path structures, and migration scripts.

#### C3. Nikki (OpenWrt Nikki / Mihomo)
- **Authority Level**: `PRIMARY_APPLICATION_REFERENCE`
- **Directory**: `upstreams/nikkinikki-OpenWrt-nikki`
- **Remote URL**: `https://github.com/nikkinikki-org/OpenWrt-nikki.git`
- **Requested Ref**: `main`
- **Actual Ref**: Branch `main`
- **HEAD SHA**: `3799926b147d7065ac98508f16951f8714e53659`
- **Latest Tag**: `v1.26.1`
- **Latest Commit Date**: `2026-08-17 15:29:45 +0800`
- **Latest Commit Title**: `fix: redundant cron task in edge cases (Joseph Mory)`
- **Git Status**: Clean
- **License**: GPL-3.0
- **Purpose**: Independent modern proxy application reference showcasing procd service management, Firewall4 nftables redirect/tproxy, ucode templating, apk packaging, and OpenWrt 24.10/25.12 compatibility.

---

### Group D: Secondary Cross-Checks

#### D1. PassWall2 (OpenWrt PassWall2)
- **Authority Level**: `SECONDARY_APPLICATION_REFERENCE`
- **Directory**: `upstreams/Openwrt-Passwall-openwrt-passwall2`
- **Remote URL**: `https://github.com/Openwrt-Passwall/openwrt-passwall2.git`
- **Requested Ref**: `main`
- **Actual Ref**: Branch `main`
- **HEAD SHA**: `cc074ed776579849ae68aec303eb85867b4dc4ee`
- **Latest Tag**: `26.8.27-1`
- **Latest Commit Date**: `2026-08-27 17:58:02 +0800`
- **Latest Commit Title**: `luci: re-add ACL process log (handsomeji)`
- **Git Status**: Clean
- **License**: GPL-3.0
- **Purpose**: Secondary reference for multi-core routing (sing-box, Xray), complex iptables/nftables firewall rules, DNS hijacking, and package lifecycle.

#### D2. Zapret-OpenWrt (Remittor Zapret OpenWrt)
- **Authority Level**: `SECONDARY_APPLICATION_REFERENCE`
- **Directory**: `upstreams/remittor-zapret-openwrt`
- **Remote URL**: `https://github.com/remittor/zapret-openwrt.git`
- **Requested Ref**: `zap1` (default branch)
- **Actual Ref**: Branch `zap1`
- **HEAD SHA**: `1b04a87558ec7965bfed0ef7fb557997872dd699`
- **Latest Tag**: `v72.20260307`
- **Latest Commit Date**: `2026-08-06 23:17:36 +0500`
- **Latest Commit Title**: `Update README.md (StressOzz)`
- **Git Status**: Clean
- **License**: MIT
- **Purpose**: OpenWrt package layout, nfqws service initialization, firewall rules, and apk/ipk packaging for zapret.

---

### Existing Repositories Verified (Unmodified)

#### Existing 1. avatarDD-zapret-gui
- **Authority Level**: `EXISTING_REFERENCE`
- **Directory**: `upstreams/avatarDD-zapret-gui`
- **Remote URL**: `https://github.com/avatarDD/zapret-gui.git`
- **Branch**: `main`
- **HEAD SHA**: `8c44df2bed98872d1348db053623ee6bf2902408`
- **Latest Commit Date**: `2026-08-17 20:27:10 +0400`
- **Git Status**: Clean

#### Existing 2. bol-van-zapret
- **Authority Level**: `EXISTING_REFERENCE`
- **Directory**: `upstreams/bol-van-zapret`
- **Remote URL**: `https://github.com/bol-van/zapret.git`
- **Branch**: `master`
- **HEAD SHA**: `87e058624c72863db53bdaf7fb6f16576dddb6ab`
- **Latest Tag**: `v72.13`
- **Latest Commit Date**: `2026-07-21 09:17:51 +0300`
- **Git Status**: Clean

#### Existing 3. bol-van-zapret2
- **Authority Level**: `EXISTING_REFERENCE`
- **Directory**: `upstreams/bol-van-zapret2`
- **Remote URL**: `https://github.com/bol-van/zapret2.git`
- **Branch**: `master`
- **HEAD SHA**: `032651deeb2117a32c67fdf5cec115d5e52a63dd`
- **Latest Commit Date**: `2026-08-02 07:01:51 +0300`
- **Git Status**: Clean

---

## 2. Key File Map for Platform & Service Comparison

```
+--------------------------------------------------------------------------------------------------+
| COMPONENT                  | SOURCE FILE / DIRECTORY                                            |
+----------------------------+--------------------------------------------------------------------+
| OPENWRT CORE               | package/system/fstools/Makefile                                    |
|                            | package/system/fstools/files/fstab.init                            |
|                            | package/system/procd/files/procd.sh                                |
|                            | package/system/procd/files/reload_config                           |
|                            | package/base-files/files/etc/rc.common                             |
|                            | package/base-files/files/sbin/sysupgrade                           |
|                            | package/base-files/files/sbin/firstboot                            |
|                            | package/base-files/files/lib/preinit/                              |
|                            | package/base-files/files/lib/functions.sh                          |
|                            | package/base-files/files/lib/functions/service.sh                  |
|                            | package/base-files/files/lib/functions/uci-defaults.sh              |
|                            | include/package-pack.mk (apk / ipk packaging logic)                |
+----------------------------+--------------------------------------------------------------------+
| FSTOOLS                    | mount_root.c (entry point for rootfs / overlay mount)              |
| (Pinned: 16718b6)          | block.c (block mount / fstab / extroot discovery)                  |
|                            | factoryreset.c (jffs2reset / firstboot / factory reset)            |
|                            | libfstools/overlay.c (overlayfs mount, ramoverlay, f2fs/ext4/jffs2)|
|                            | libfstools/extroot.c (extroot mounting on USB/SATA/eMMC)           |
|                            | libfstools/mount.c & rootdisk.c (root disk detection)              |
|                            | libfstools/snapshot.c (overlay snapshotting)                       |
|                            | libblkid-tiny/ (lightweight filesystem probe & UUID/label detection|
+----------------------------+--------------------------------------------------------------------+
| FRIENDLYWRT                | target/linux/rockchip/armv8/base-files/root/setup.sh               |
| (RK3576 / Rockchip)        | target/linux/rockchip/armv8/base-files/lib/preinit/79_move_config  |
|                            | target/linux/rockchip/armv8/base-files/lib/upgrade/platform.sh     |
|                            | target/linux/rockchip/armv8/base-files/etc/uci-defaults/           |
|                            | target/linux/rockchip/armv8/base-files/etc/sysctl.d/               |
|                            | target/linux/rockchip/armv8/base-files/usr/bin/fa-fancontrol.sh    |
|                            | target/linux/rockchip/image/armv8.mk (Image generation / layout)   |
+----------------------------+--------------------------------------------------------------------+
| OPENWRT SING-BOX           | net/sing-box/Makefile (USERID declaration, build flags)            |
| (Official 25.12 Feed)      | net/sing-box/files/sing-box.init (procd service script)            |
|                            | net/sing-box/files/sing-box.conf (default UCI configuration)       |
+----------------------------+--------------------------------------------------------------------+
| MOMO                       | momo/Makefile (Package metadata, dependencies, apk definitions)    |
| (Primary Sing-box App)     | momo/files/momo.init (procd service definition, instance lifecycle)|
|                            | momo/files/momo.conf (UCI config specification)                    |
|                            | momo/files/momo.upgrade (Upgrade migration hook)                   |
|                            | momo/files/scripts/firewall_include.sh (Firewall4 nftables hooks)  |
|                            | momo/files/scripts/include.sh (Core helper functions)              |
|                            | momo/files/uci-defaults/ (Default uci setup and migrations)        |
|                            | momo/files/ucode/ (ucode template generation & mixin configs)      |
|                            | luci-app-momo/Makefile & htdocs/ (LuCI JavaScript UI layer)        |
|                            | install.sh & uninstall.sh (Standalone installer script)            |
+----------------------------+--------------------------------------------------------------------+
| HOMEPROXY                  | Makefile (Package definition, conffiles, capabilities)             |
| (Primary Sing-box App)     | root/etc/init.d/homeproxy (procd service instance management)      |
|                            | root/etc/config/homeproxy (UCI data model)                         |
|                            | root/etc/capabilities/homeproxy.json (procd capabilities)          |
|                            | root/etc/homeproxy/scripts/homeproxy.uc (sing-box JSON generator)  |
|                            | root/etc/homeproxy/scripts/firewall_pre.uc (NFTables rules engine) |
|                            | root/etc/homeproxy/scripts/firewall_post.sh (Post-firewall setup)  |
|                            | root/etc/homeproxy/scripts/generate_client.uc (Outbound config gen)|
|                            | root/etc/homeproxy/scripts/generate_server.uc (Inbound config gen) |
|                            | root/etc/homeproxy/scripts/migrate_config.uc (Schema migrations)   |
|                            | root/etc/uci-defaults/luci-homeproxy (Installation init hook)      |
+----------------------------+--------------------------------------------------------------------+
| NIKKI                      | nikki/Makefile (Package build rules, Mihomo integration)           |
| (Primary Mihomo App)       | nikki/files/nikki.init (procd service definition)                  |
|                            | nikki/files/nikki.conf (UCI configuration)                         |
|                            | nikki/files/nftables/ (NFTables rulesets)                          |
|                            | nikki/files/scripts/firewall_include.sh (Firewall4 integration)    |
|                            | nikki/files/ucode/ (ucode config generation)                       |
|                            | luci-app-nikki/ (LuCI interface files)                            |
+----------------------------+--------------------------------------------------------------------+
| PASSWALL2                  | luci-app-passwall2/Makefile (Package declaration)                  |
| (Secondary Cross-Check)    | luci-app-passwall2/root/etc/init.d/passwall2 (Service init)        |
|                            | luci-app-passwall2/root/usr/share/passwall2/ (Core scripts)        |
|                            | luci-app-passwall2/root/usr/share/passwall2/nftables.sh            |
|                            | luci-app-passwall2/root/usr/share/passwall2/app.sh                 |
+----------------------------+--------------------------------------------------------------------+
| ZAPRET-OPENWRT             | zapret/Makefile (Zapret build definition)                          |
| (Secondary Cross-Check)    | zapret/init.d.sh (Procd / sysvinit compatibility script)           |
|                            | zapret/def-cfg.sh & config.default (Default configuration)         |
|                            | zapret/uci-def-cfg.sh (UCI default setup)                          |
|                            | luci-app-zapret/Makefile & root/ (LuCI management layer)           |
+----------------------------+--------------------------------------------------------------------+
```
