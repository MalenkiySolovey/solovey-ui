#!/usr/bin/env bash

# Package-phase process boundary shared by the canonical driver and its
# behavioral provenance tests. The caller supplies values, not environment
# authority: every child starts from an empty environment and the launcher is
# an absolute trusted-base executable.

openwrt_package_exec() {
	local process_path=${1:?package PATH is required}
	local process_home=${2:?package HOME is required}
	local process_xdg_config=${3:?package XDG config root is required}
	local process_xdg_cache=${4:?package XDG cache root is required}
	local process_tmp=${5:?package temporary root is required}
	shift 5
	[[ $# -gt 0 ]] || { echo '[openwrt-package-process] command is required' >&2; return 64; }

	/usr/bin/env -i \
		"PATH=$process_path" \
		"HOME=$process_home" \
		"XDG_CONFIG_HOME=$process_xdg_config" \
		"XDG_CACHE_HOME=$process_xdg_cache" \
		"TMPDIR=$process_tmp" \
		'SHELL=/usr/bin/bash' \
		'TZ=UTC' \
		'LC_ALL=C' \
		'LANG=C' \
		"$@"
}

openwrt_package_make() {
	local package_make_program=${1:?Make program is required}
	local make_path=${2:?package PATH is required}
	local make_home=${3:?package HOME is required}
	local make_xdg_config=${4:?package XDG config root is required}
	local make_xdg_cache=${5:?package XDG cache root is required}
	local make_tmp=${6:?package temporary root is required}
	shift 6
	[[ "$package_make_program" == '/usr/bin/make' ]] || {
		echo "[openwrt-package-process] non-canonical Make program: $package_make_program" >&2
		return 64
	}
	openwrt_package_exec "$make_path" "$make_home" "$make_xdg_config" \
		"$make_xdg_cache" "$make_tmp" "$package_make_program" "$@"
}
