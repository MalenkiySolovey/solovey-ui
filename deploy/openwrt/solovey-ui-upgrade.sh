# Sourced by OpenWrt /lib/upgrade after sysupgrade_init_conffiles is selected.
# Preparation is deliberately prepended so keep.d discovery sees the newly
# completed logical snapshot and metadata.

solovey_ui_preservation_helper() {
	local helper=/usr/lib/solovey-ui/solovey-openwrt-preservation
	[ -x "$helper" ] || {
		echo "Solovey UI preservation helper is unavailable" >&2
		return 1
	}
	SUI_DB_FOLDER=/etc/solovey-ui/db \
	SUI_DEPLOYMENT_KIND=openwrt-package-managed \
	SUI_COMPONENTS_INSTALLED_FILE=/usr/lib/solovey-ui/components/installed.json \
		start-stop-daemon -S -x "$helper" -c solovey-ui:solovey-ui -- "$@" 1>&2
}

solovey_ui_prepare_preservation() {
	[ "${CONF_BACKUP_LIST:-0}" -eq 0 ] || return 0
	[ "${SOLOVEY_UI_PRESERVATION_PREPARED:-0}" -eq 0 ] || return 0
	[ -n "${IMAGE:-}" ] || [ -n "${CONF_BACKUP:-}" ] || return 0
	[ "${SAVE_CONFIG:-1}" -eq 1 ] || {
		echo "Solovey UI requires configuration preservation for a supported sysupgrade" >&2
		exit 1
	}

	solovey_ui_preservation_helper prepare || {
		echo "Solovey UI logical preservation failed; sysupgrade is blocked" >&2
		exit 1
	}
	SOLOVEY_UI_PRESERVATION_PREPARED=1
	trap 'solovey_ui_finalize_preservation_backup "$?"' EXIT
}

solovey_ui_finalize_preservation_backup() {
	local status="$1"
	local action=fail-backup
	trap - EXIT
	[ "$status" -ne 0 ] || action=complete-backup
	solovey_ui_preservation_helper "$action" ||
		echo "Solovey UI preservation cleanup is deferred to startup reconciliation" >&2
	exit "$status"
}

# List mode must describe the dynamic preservation members without creating
# them. The exact native backup path prepares those same fixed members before
# OpenWrt builds the list, so both commands expose one canonical inventory.
solovey_ui_declare_preservation_inventory() {
	[ "${CONF_BACKUP_LIST:-0}" -eq 1 ] || return 0
	printf '%s\n' \
		/etc/solovey-ui/db/sysupgrade-preservation/database.db \
		/etc/solovey-ui/db/sysupgrade-preservation/metadata.json >> "$1"
}

solovey_ui_validate_preservation_inventory() {
	[ "${CONF_BACKUP_LIST:-0}" -eq 0 ] || return 0
	[ "${SOLOVEY_UI_PRESERVATION_PREPARED:-0}" -eq 1 ] || {
		echo "Solovey UI preservation evidence is incomplete" >&2
		exit 1
	}
	awk '
		$0 == "/etc/solovey-ui/openwrt-instance-id" { instance++; next }
		$0 == "/etc/solovey-ui/db/sysupgrade-preservation/database.db" { snapshot++; next }
		$0 == "/etc/solovey-ui/db/sysupgrade-preservation/metadata.json" { metadata++; next }
		$0 == "/etc/solovey-ui/db" || index($0, "/etc/solovey-ui/db/") == 1 { unexpected=1 }
		END { exit !(instance == 1 && snapshot == 1 && metadata == 1 && unexpected == 0) }
	' "$1" || {
		echo "Solovey UI sysupgrade inventory contains missing or unmanaged database state" >&2
		exit 1
	}
}

# Native `sysupgrade -r` extracts directly into the live root and has no
# package-owned quiesce/reopen transaction for the logical database. Reject it
# while this sourced hook still precedes OpenWrt's archive extraction; a zero
# exit that leaves the current database unchanged would be false support.
[ -z "${CONF_RESTORE:-}" ] || {
	echo "Solovey UI does not support native sysupgrade backup restore; no files were applied" >&2
	exit 1
}

# Firmware upgrades that would bypass native archive construction or permit an
# interactive/overlay decision to discard or over-select config are outside
# the package contract.
# Refuse them before image validation can reach the destructive transition.
if [ -n "${IMAGE:-}" ] && [ "${TEST:-0}" -eq 0 ]; then
	[ -z "${CONF_IMAGE:-}" ] || {
		echo "Solovey UI does not support sysupgrade with an externally supplied config archive" >&2
		exit 1
	}
	[ "${INTERACTIVE:-0}" -eq 0 ] || {
		echo "Solovey UI requires non-interactive configuration preservation" >&2
		exit 1
	}
	[ "${SAVE_OVERLAY:-0}" -eq 0 ] || {
		echo "Solovey UI does not support broad overlay preservation" >&2
		exit 1
	}
	solovey_ui_prepare_preservation
fi

sysupgrade_init_conffiles="solovey_ui_prepare_preservation $sysupgrade_init_conffiles solovey_ui_declare_preservation_inventory solovey_ui_validate_preservation_inventory"
