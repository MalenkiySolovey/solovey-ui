#!/usr/bin/env bash

# Build-only OpenWrt target coordinates.  This file is intentionally limited
# to SDK, compiler, Go and artifact facts; it must never be sourced by product
# runtime code.

openwrt_target_profile_load() {
	local profile=${1:-}
	case "$profile" in
		x86-64|x86_64)
			OPENWRT_PROFILE_KEY='x86-64'
			OPENWRT_RELEASE='25.12.5'
			OPENWRT_SOURCE_TAG='v25.12.5'
			OPENWRT_REVISION='r33051-f5dae5ece4'
			OPENWRT_SOURCE_COMMIT='f0a60eee2fe051741c643ea6118718aae1ef17fb'
			OPENWRT_TARGET='x86'
			OPENWRT_SUBTARGET='64'
			OPENWRT_PACKAGE_ARCH='x86_64'
			OPENWRT_SDK_FILENAME='openwrt-sdk-25.12.5-x86-64_gcc-14.3.0_musl.Linux-x86_64.tar.zst'
			OPENWRT_SDK_SHA256='0c8df0151a1e88feb7c03d694d61f6a18d51872815b7c811d76e2b77504d5e9c'
			OPENWRT_SDK_DOWNLOAD='https://downloads.openwrt.org/releases/25.12.5/targets/x86/64/openwrt-sdk-25.12.5-x86-64_gcc-14.3.0_musl.Linux-x86_64.tar.zst'
			OPENWRT_SDK_TOPLEVEL='openwrt-sdk-25.12.5-x86-64_gcc-14.3.0_musl.Linux-x86_64'
			OPENWRT_GOARCH='amd64'
			OPENWRT_GOARCH_VARIANT='v1'
			OPENWRT_PLATFORM='amd64'
			OPENWRT_CC_TARGET='x86_64-openwrt-linux-musl'
			OPENWRT_TOOLCHAIN_DIR='staging_dir/toolchain-x86_64_gcc-14.3.0_musl'
			OPENWRT_CONFIG_TARGET='CONFIG_TARGET_x86=y'
			OPENWRT_CONFIG_SUBTARGET='CONFIG_TARGET_x86_64=y'
			OPENWRT_CONFIG_ARCH='CONFIG_TARGET_ARCH_PACKAGES="x86_64"'
			OPENWRT_ELF_FILE_PATTERN='ELF 64-bit LSB executable, x86-64'
			OPENWRT_ELF_MACHINE_PATTERN='Advanced Micro Devices X86-64'
			;;
		rockchip-armv8|rockchip/armv8|aarch64_generic)
			OPENWRT_PROFILE_KEY='rockchip-armv8'
			OPENWRT_RELEASE='25.12.5'
			OPENWRT_SOURCE_TAG='v25.12.5'
			OPENWRT_REVISION='r33051-f5dae5ece4'
			OPENWRT_SOURCE_COMMIT='f0a60eee2fe051741c643ea6118718aae1ef17fb'
			OPENWRT_TARGET='rockchip'
			OPENWRT_SUBTARGET='armv8'
			OPENWRT_PACKAGE_ARCH='aarch64_generic'
			OPENWRT_SDK_FILENAME='openwrt-sdk-25.12.5-rockchip-armv8_gcc-14.3.0_musl.Linux-x86_64.tar.zst'
			OPENWRT_SDK_SHA256='59194a023968398af64bfa7d8bc3eac322641f6dc9cdbade28a4d9dd41866eba'
			OPENWRT_SDK_DOWNLOAD='https://downloads.openwrt.org/releases/25.12.5/targets/rockchip/armv8/openwrt-sdk-25.12.5-rockchip-armv8_gcc-14.3.0_musl.Linux-x86_64.tar.zst'
			OPENWRT_SDK_TOPLEVEL='openwrt-sdk-25.12.5-rockchip-armv8_gcc-14.3.0_musl.Linux-x86_64'
			OPENWRT_GOARCH='arm64'
			OPENWRT_GOARCH_VARIANT=''
			OPENWRT_PLATFORM='arm64'
			OPENWRT_CC_TARGET='aarch64-openwrt-linux-musl'
			OPENWRT_TOOLCHAIN_DIR='staging_dir/toolchain-aarch64_generic_gcc-14.3.0_musl'
			OPENWRT_CONFIG_TARGET='CONFIG_TARGET_rockchip=y'
			OPENWRT_CONFIG_SUBTARGET='CONFIG_TARGET_rockchip_armv8=y'
			OPENWRT_CONFIG_ARCH='CONFIG_TARGET_ARCH_PACKAGES="aarch64_generic"'
			OPENWRT_ELF_FILE_PATTERN='ELF 64-bit LSB executable, ARM aarch64'
			OPENWRT_ELF_MACHINE_PATTERN='AArch64'
			;;
		*)
			echo "[openwrt-target-profile] ERROR: unsupported target profile: $profile" >&2
			return 2
			;;
	esac
}

