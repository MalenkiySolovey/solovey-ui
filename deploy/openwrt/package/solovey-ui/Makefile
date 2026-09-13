# SPDX-License-Identifier: GPL-3.0-only
#
# This package is built by an exact OpenWrt SDK. The build driver supplies a
# source-bound, target-compiled rootfs payload; this recipe owns only the
# OpenWrt package metadata and lifecycle hooks.

include $(TOPDIR)/rules.mk

SOLOVEY_UI_SOURCE_DIR:=$(strip $(SOLOVEY_UI_SOURCE_DIR))
SOLOVEY_UI_STAGE_DIR:=$(strip $(SOLOVEY_UI_STAGE_DIR))
SOLOVEY_UI_SOURCE_DATE:=$(strip $(SOLOVEY_UI_SOURCE_DATE))
SOLOVEY_UI_NODE_PROGRAM:=$(strip $(SOLOVEY_UI_NODE_PROGRAM))

ifeq ($(SOLOVEY_UI_SOURCE_DIR),)
$(error SOLOVEY_UI_SOURCE_DIR must name the checked source worktree)
endif
ifeq ($(SOLOVEY_UI_STAGE_DIR),)
$(error SOLOVEY_UI_STAGE_DIR must name the source-bound OpenWrt staging directory)
endif
ifeq ($(SOLOVEY_UI_SOURCE_DATE),)
$(error SOLOVEY_UI_SOURCE_DATE must be a deterministic YYYY-MM-DD source date)
endif
ifeq ($(SOLOVEY_UI_NODE_PROGRAM),)
$(error SOLOVEY_UI_NODE_PROGRAM must name the stage-bound Node executable)
endif

PKG_NAME:=solovey-ui
PKG_VERSION:=$(shell sed -n '1p' $(SOLOVEY_UI_SOURCE_DIR)/config/identity/version)
PKG_RELEASE:=21
PKG_SOURCE_DATE:=$(SOLOVEY_UI_SOURCE_DATE)
PKG_LICENSE:=GPL-3.0-only
PKG_LICENSE_FILES:=LICENSE
PKG_MAINTAINER:=MalenkiySolovey
PKG_URL:=https://github.com/MalenkiySolovey/solovey-ui
PKG_BUILD_DIR:=$(BUILD_DIR)/$(PKG_NAME)-$(PKG_VERSION)

include $(INCLUDE_DIR)/package.mk

define Package/solovey-ui
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Solovey UI management panel for OpenWrt
  # Direct dependencies deliberately mirror every mandatory runtime facility
  # invoked by the package.  Do not rely on the usual base image or on a
  # transitive dependency: stripped OpenWrt 25.12.5 images receive the same
  # /bin/ubus, /sbin/uci, logread and the common /usr/sbin/nft CLI contract.
  # The nftables capability deliberately leaves JSON-vs-no-JSON provider
  # selection to OpenWrt and packages such as firewall4.
  DEPENDS:=+procd +dropbear +ubus +uci +logd +nftables
  USERID:=solovey-ui:solovey-ui
endef

define Package/solovey-ui/description
  Solovey UI management panel packaged for the OpenWrt 25.12 apk backend.
  The package owns its fixed procd topology and exact logical sysupgrade
  preservation integration. Runtime self-replacement remains package-managed.
endef

define Build/Prepare
	$(INSTALL_DIR) $(PKG_BUILD_DIR)
endef

define Build/Compile
	$(SHELL) $(TOPDIR)/package/$(PKG_NAME)/files/validate-stage.sh $(SOLOVEY_UI_SOURCE_DIR) $(SOLOVEY_UI_STAGE_DIR) $(SOLOVEY_UI_NODE_PROGRAM)
endef

define Package/solovey-ui/install
	$(SHELL) $(TOPDIR)/package/$(PKG_NAME)/files/validate-stage.sh $(SOLOVEY_UI_SOURCE_DIR) $(SOLOVEY_UI_STAGE_DIR) $(SOLOVEY_UI_NODE_PROGRAM)
	$(INSTALL_DIR) $(1)
	$(CP) $(SOLOVEY_UI_STAGE_DIR)/payload/. $(1)/
endef

define Package/solovey-ui/postinst
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] && exit 0
/usr/lib/solovey-ui/solovey-openwrt-lifecycle reconcile
endef

define Package/solovey-ui/preinst
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] && exit 0
[ "$${PKG_UPGRADE:-0}" = 1 ] || exit 0
if [ -x /etc/init.d/solovey-ui ]; then
	/etc/init.d/solovey-ui stop || exit 1
fi
attempt=0
while [ "$$attempt" -lt 30 ]; do
	remaining=0
	for process in /proc/[0-9]*/exe; do
		[ -e "$$process" ] || continue
		executable=$$(readlink "$$process" 2>/dev/null || true)
		case "$$executable" in
			/usr/lib/solovey-ui|/usr/lib/solovey-ui/*) remaining=1; break ;;
		esac
	done
	[ "$$remaining" = 0 ] && exit 0
	attempt=$$((attempt + 1))
	sleep 1
done
exit 1
endef

define Package/solovey-ui/prerm
#!/bin/sh
[ -n "$${IPKG_INSTROOT}" ] && exit 0
/usr/lib/solovey-ui/solovey-openwrt-lifecycle pre-remove
endef

$(eval $(call BuildPackage,solovey-ui))
