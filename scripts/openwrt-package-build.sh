#!/usr/bin/env bash

set -Eeuo pipefail

# Packages one canonical, target-compiled stage through a fresh private
# extraction of the verified official SDK archive. It does not build a second
# release profile, boot OpenWrt, or create an operating-system image.

readonly GO_TOOLCHAIN_FILENAME='go1.26.6.linux-amd64.tar.gz'
readonly GO_TOOLCHAIN_SHA256='708effb774be8237570d0add163225abbdfaf4fca28b2611df167beba4feef89'
readonly OPENWRT_MAKE_PROGRAM='/usr/bin/make'
readonly TRUSTED_BASE_PATH='/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin'

sdk_archive=''
source_dir=''
out_dir=''
jobs='1'
trust_roots_file=''
go_toolchain_archive=''
temporary_root=''
target_profile='x86-64'

source "$(dirname "${BASH_SOURCE[0]}")/openwrt-target-profile.sh"

usage() {
	cat <<'EOF'
Usage: scripts/openwrt-package-build.sh --sdk-archive <official-sdk.tar.zst> --go-toolchain-archive <go1.26.6.linux-amd64.tar.gz> --source <worktree> --trust-roots-file <public-base64-file> --out <new-output-dir> [--jobs <count>] [--target-profile <x86-64|rockchip-armv8>]

This entrypoint invokes the source-owned canonical stage producer itself. It
does not accept caller-supplied product binaries or a caller-selected stage.
It verifies and extracts the official SDK archive into a fresh private root,
executes package construction only there, then proves the APK rootfs is equal
to the canonical stage. It creates no OpenWrt runtime.
EOF
}

fail() {
	echo "[openwrt-package-build] ERROR: $*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--sdk-archive) sdk_archive=${2:?--sdk-archive requires a value}; shift 2 ;;
		--go-toolchain-archive) go_toolchain_archive=${2:?--go-toolchain-archive requires a value}; shift 2 ;;
		--source) source_dir=${2:?--source requires a value}; shift 2 ;;
		--trust-roots-file) trust_roots_file=${2:?--trust-roots-file requires a value}; shift 2 ;;
		--out) out_dir=${2:?--out requires a value}; shift 2 ;;
		--jobs) jobs=${2:?--jobs requires a value}; shift 2 ;;
		--target-profile) target_profile=${2:?--target-profile requires a value}; shift 2 ;;
		--help|-h) usage; exit 0 ;;
		*) usage; fail "unknown argument: $1" ;;
	esac
done

openwrt_target_profile_load "$target_profile" || exit $?

[[ -n "$sdk_archive" && -n "$go_toolchain_archive" && -n "$source_dir" && -n "$trust_roots_file" && -n "$out_dir" ]] || { usage; exit 2; }
[[ "$jobs" =~ ^[1-9][0-9]*$ ]] || fail '--jobs must be a positive integer'
for variable in MAKE MAKEFLAGS GNUMAKEFLAGS MFLAGS MAKELEVEL MAKEFILES MAKE_RESTARTS MAKE_TERMOUT MAKE_TERMERR CONFIG_SITE \
	SOURCE_DATE_EPOCH STAGING_DIR BUILD_DIR TOPDIR; do
	if [[ -v "$variable" ]]; then
		fail "forbidden package-build environment override is set: $variable"
	fi
done

# Node remains the already-bound stage tool. All other package-layer base tools
# come from fixed absolute build-runner locations; the caller's PATH is removed
# before any archive, SDK, Make or artifact command is executed.
node_candidate=$(command -v node) || fail 'required Node executable is unavailable'
[[ "$node_candidate" == /* ]] || fail 'Node executable must resolve to an absolute file, not a caller shell function'
readonly node_program="$(/usr/bin/readlink -f "$node_candidate")"
[[ -f "$node_program" && -x "$node_program" ]] || fail 'resolved Node executable is unavailable'
readonly stage_tool_path="$(dirname "$node_program"):$TRUSTED_BASE_PATH"
export PATH="$TRUSTED_BASE_PATH"
for program in /usr/bin/awk /usr/bin/bash /usr/bin/cp /usr/bin/env /usr/bin/file /usr/bin/find \
	/usr/bin/git /usr/bin/make /usr/bin/readelf /usr/bin/readlink /usr/bin/sha256sum \
	/usr/bin/sort /usr/bin/tar /usr/bin/zstd; do
	[[ -f "$program" && -x "$program" ]] || fail "required trusted-base executable is unavailable: $program"
done

sdk_archive=$(/usr/bin/readlink -f "$sdk_archive")
go_toolchain_archive=$(/usr/bin/readlink -f "$go_toolchain_archive")
source_dir=$(/usr/bin/readlink -f "$source_dir")
trust_roots_file=$(/usr/bin/readlink -f "$trust_roots_file")
[[ -f "$sdk_archive" && -f "$go_toolchain_archive" && -d "$source_dir" && -f "$trust_roots_file" ]] || fail 'SDK archive, Go toolchain archive, source worktree or public trust roots are unavailable'
[[ "$(git -C "$source_dir" rev-parse --is-inside-work-tree)" == 'true' ]] || fail 'source directory is not a Git worktree'
[[ ! -e "$out_dir" ]] || fail "output directory already exists: $out_dir"

temporary_root=$(mktemp -d)
trap 'rm -rf "${temporary_root:-}"' EXIT
readonly extraction_root="$temporary_root/sdk-extraction"
readonly sdk_derivation="$temporary_root/SDK_DERIVATION.json"
readonly package_host_tool_proof="$temporary_root/PACKAGE_HOST_TOOL_AUTHORITY.json"
readonly package_home="$temporary_root/home"
readonly package_tmp="$temporary_root/tmp"
readonly package_xdg_config="$temporary_root/xdg-config"
readonly package_xdg_cache="$temporary_root/xdg-cache"
readonly private_sdk_archive="$temporary_root/$OPENWRT_SDK_FILENAME"
readonly private_go_archive="$temporary_root/$GO_TOOLCHAIN_FILENAME"
mkdir -m 0700 "$extraction_root" "$package_home" "$package_tmp" "$package_xdg_config" "$package_xdg_cache"

# Caller paths are input streams only. The archive is copied into the private
# derivation root before its digest, member list or contents are trusted, and
# the caller path is never reopened after this point.
"$node_program" "$source_dir/scripts/openwrt-sdk-archive.mjs" pin-archive \
	--source "$sdk_archive" --out "$private_sdk_archive" --sha256 "$OPENWRT_SDK_SHA256"
"$node_program" "$source_dir/scripts/openwrt-go-authority.mjs" pin-archive \
	--source "$go_toolchain_archive" --out "$private_go_archive" --sha256 "$GO_TOOLCHAIN_SHA256"

/usr/bin/tar --zstd -tf "$private_sdk_archive" | "$node_program" "$source_dir/scripts/openwrt-sdk-archive.mjs" validate-list \
	--expected-top "$OPENWRT_SDK_TOPLEVEL"
/usr/bin/tar --zstd --extract --file "$private_sdk_archive" --directory "$extraction_root" --no-same-owner
"$node_program" "$source_dir/scripts/openwrt-sdk-archive.mjs" verify-extracted \
	--root "$extraction_root" --expected-top "$OPENWRT_SDK_TOPLEVEL" \
	--archive-filename "$OPENWRT_SDK_FILENAME" --archive-sha256 "$OPENWRT_SDK_SHA256" \
	--out "$sdk_derivation"
readonly sdk="$extraction_root/$OPENWRT_SDK_TOPLEVEL"
readonly make_program="$OPENWRT_MAKE_PROGRAM"
# OpenWrt derives TARGET_PATH from this fixed base and prepends its authenticated
# host/hostpkg directories only for package rules. Supplying those SDK paths at
# top level would expose pristine-archive buildbot symlinks before defconfig has
# canonically refreshed its host-command links.
readonly package_path="$TRUSTED_BASE_PATH"

# Source the package-only process primitive before the final SDK proof. Calling
# its shell function later adds no script interpreter between tree verification
# and the fixed /usr/bin/env -> /usr/bin/make boundary.
source "$source_dir/scripts/openwrt-package-process.sh"

run_package_command() {
	openwrt_package_exec "$package_path" "$package_home" "$package_xdg_config" \
		"$package_xdg_cache" "$package_tmp" "$@"
}

run_package_make() {
	openwrt_package_make "$make_program" "$package_path" "$package_home" "$package_xdg_config" \
		"$package_xdg_cache" "$package_tmp" "$@"
}

"$node_program" "$source_dir/scripts/openwrt-package-host-tools.mjs" create \
	--make "$make_program" --node "$node_program" --out "$package_host_tool_proof"
"$node_program" "$source_dir/scripts/openwrt-package-host-tools.mjs" verify \
	--make "$make_program" --node "$node_program" --proof "$package_host_tool_proof"

[[ -f "$sdk/include/package.mk" && -f "$sdk/include/package-pack.mk" && -x "$sdk/scripts/feeds" ]] || fail 'SDK does not expose the required package build surface'
grep -q 'apk mkpkg' "$sdk/include/package-pack.mk" || fail 'SDK does not use the apk package backend'
umask 022
run_package_make -C "$sdk" defconfig
if grep -qx '# CONFIG_NO_STRIP is not set' "$sdk/.config"; then
	sed -i \
		-e 's/^# CONFIG_NO_STRIP is not set$/CONFIG_NO_STRIP=y/' \
		-e 's/^CONFIG_USE_STRIP=y$/# CONFIG_USE_STRIP is not set/' \
		-e 's/^CONFIG_USE_SSTRIP=y$/# CONFIG_USE_SSTRIP is not set/' \
		"$sdk/.config"
elif ! grep -qx 'CONFIG_NO_STRIP=y' "$sdk/.config"; then
	printf '%s\n' 'CONFIG_NO_STRIP=y' >> "$sdk/.config"
fi
run_package_make -C "$sdk" defconfig

# OpenWrt's metadata refresh leaves four content-stable scratch files whose
# names contain the host PID. They are not build inputs. Remove only that
# pinned 25.12.5 shape before representing the complete prepared SDK tree;
# any changed or additional shape remains represented (or fails here).
readonly sdk_info="$sdk/tmp/info"
[[ -d "$sdk_info" ]] || fail 'SDK defconfig did not create tmp/info'
defconfig_scratch=()
while IFS= read -r -d '' name; do
	if [[ "$name" =~ ^\.(files|overrides)-(package|target)info-[0-9]+$ ]]; then
		defconfig_scratch+=("$sdk_info/$name")
	fi
done < <(find "$sdk_info" -mindepth 1 -maxdepth 1 -type f -printf '%f\0')
[[ ${#defconfig_scratch[@]} -eq 4 ]] || fail "expected exactly four OpenWrt defconfig PID scratch files, found ${#defconfig_scratch[@]}"
rm -f -- "${defconfig_scratch[@]}"

grep -Fxq 'VERSION_NUMBER:=$(if $(VERSION_NUMBER),$(VERSION_NUMBER),25.12.5)' "$sdk/include/version.mk" || fail 'SDK release is not 25.12.5'
grep -qx "$OPENWRT_CONFIG_TARGET" "$sdk/.config" || fail "SDK target does not match profile $OPENWRT_PROFILE_KEY"
grep -qx "$OPENWRT_CONFIG_SUBTARGET" "$sdk/.config" || fail "SDK subtarget does not match profile $OPENWRT_PROFILE_KEY"
grep -qx "$OPENWRT_CONFIG_ARCH" "$sdk/.config" || fail "SDK package architecture does not match profile $OPENWRT_PROFILE_KEY"
grep -qx 'CONFIG_NO_STRIP=y' "$sdk/.config" || fail 'SDK package stripping is not disabled for stage equivalence'

mkdir -p "$out_dir"
readonly artifact_dir="$out_dir/artifacts"
readonly stage_dir="$out_dir/canonical-stage"
mkdir -p "$artifact_dir"
cp "$sdk_derivation" "$artifact_dir/SDK_EXTRACTED_AUTHORITY.json"
cp "$package_host_tool_proof" "$artifact_dir/PACKAGE_HOST_TOOL_AUTHORITY.json"

sysroot="$sdk/$OPENWRT_TOOLCHAIN_DIR"
[[ -d "$sysroot" ]] || fail "SDK sysroot is unavailable for profile $OPENWRT_PROFILE_KEY"
[[ -n "${CC:-}" && -n "${CXX:-}" ]] || fail 'CC and CXX compiler programs are required'
read -r -a caller_cc <<< "$CC"
read -r -a caller_cxx <<< "$CXX"
[[ ${#caller_cc[@]} -ge 1 && ${#caller_cxx[@]} -ge 1 ]] || fail 'CC and CXX compiler programs are invalid'
readonly caller_cc_program="$(/usr/bin/readlink -f "${caller_cc[0]}")"
readonly caller_cxx_program="$(/usr/bin/readlink -f "${caller_cxx[0]}")"
[[ -x "$caller_cc_program" && -x "$caller_cxx_program" ]] || fail 'resolved CC/CXX compiler programs are unavailable'
stage_cc="$caller_cc_program --target=$OPENWRT_CC_TARGET --sysroot=$sysroot"
stage_cxx="$caller_cxx_program --target=$OPENWRT_CC_TARGET --sysroot=$sysroot"
PATH="$stage_tool_path" LC_ALL=C.UTF-8 LANG=C.UTF-8 GOOS=linux GOARCH="$OPENWRT_GOARCH" CGO_ENABLED=1 \
	CC="$stage_cc" CXX="$stage_cxx" CGO_LDFLAGS=-fuse-ld=lld \
	/usr/bin/bash "$source_dir/scripts/openwrt-stage-build.sh" \
	--source "$source_dir" --out "$stage_dir" --trust-roots-file "$trust_roots_file" \
	--go-toolchain-archive "$private_go_archive" --target-profile "$target_profile"
"$node_program" "$source_dir/scripts/openwrt-stage-manifest.mjs" verify --stage "$stage_dir" --source-root "$source_dir"

for binary in \
	solovey-ui \
	solovey-privileged-broker \
	solovey-ssh-proof \
	solovey-evidence \
	solovey-broker-readiness \
	solovey-openwrt-preservation \
	solovey-openwrt-owner-manifest \
	solovey-openwrt-broker-manifest \
	solovey-openwrt-durability \
	solovey-openwrt-lifecycle
do
	file_output=$(file "$stage_dir/payload/usr/lib/solovey-ui/$binary")
	grep -Fq "$OPENWRT_ELF_FILE_PATTERN" <<< "$file_output" || fail "wrong executable architecture: $binary"
	readelf_output=$(readelf -h "$stage_dir/payload/usr/lib/solovey-ui/$binary")
	grep -Fq "$OPENWRT_ELF_MACHINE_PATTERN" <<< "$readelf_output" || fail "wrong ELF machine: $binary"
done

source_date=$("$node_program" -e "const f=require(process.argv[1]); process.stdout.write(f.build.facts.revision.sourceDate)" \
	"$stage_dir/payload/usr/lib/solovey-ui/SOURCE_FINGERPRINT.json")
package_dir="$sdk/package/solovey-ui"
[[ ! -e "$package_dir" ]] || fail "SDK is not clean: package path exists: $package_dir"
cp -a "$source_dir/deploy/openwrt/package/solovey-ui" "$package_dir"

readonly prepared_sdk_derivation="$temporary_root/SDK_DERIVATION.json"
"$node_program" "$source_dir/scripts/openwrt-sdk-archive.mjs" create-tree \
	--root "$sdk" --expected-top "$OPENWRT_SDK_TOPLEVEL" --archive-filename "$OPENWRT_SDK_FILENAME" \
	--archive-sha256 "$OPENWRT_SDK_SHA256" --phase prepared-for-package --out "$prepared_sdk_derivation"
cp "$prepared_sdk_derivation" "$artifact_dir/SDK_DERIVATION.json"

grep -qx "$OPENWRT_CONFIG_TARGET" "$sdk/.config" || fail 'SDK target changed after defconfig'
grep -qx "$OPENWRT_CONFIG_SUBTARGET" "$sdk/.config" || fail 'SDK subtarget changed after defconfig'
grep -qx "$OPENWRT_CONFIG_ARCH" "$sdk/.config" || fail 'SDK package architecture changed after defconfig'
grep -qx 'CONFIG_NO_STRIP=y' "$sdk/.config" || fail 'SDK package stripping policy changed after defconfig'
"$node_program" "$source_dir/scripts/openwrt-package-host-tools.mjs" verify \
	--make "$make_program" --node "$node_program" --proof "$package_host_tool_proof" \
	--source-fingerprint "$stage_dir/payload/usr/lib/solovey-ui/SOURCE_FINGERPRINT.json"
"$node_program" "$source_dir/scripts/openwrt-sdk-archive.mjs" verify-tree --root "$sdk" --proof "$prepared_sdk_derivation"
run_package_make -C "$sdk" -j"$jobs" package/solovey-ui/compile V=s \
	SOLOVEY_UI_SOURCE_DIR="$source_dir" SOLOVEY_UI_STAGE_DIR="$stage_dir" SOLOVEY_UI_SOURCE_DATE="$source_date" \
	SOLOVEY_UI_NODE_PROGRAM="$node_program"
run_package_make -C "$sdk" package/index

mapfile -t packages < <(/usr/bin/find "$sdk/bin" -type f -name 'solovey-ui-*.apk' -print | /usr/bin/sort)
[[ ${#packages[@]} -eq 1 ]] || fail "expected exactly one APK, found ${#packages[@]}"
artifact=${packages[0]}
artifact_copy="$artifact_dir/$(basename "$artifact")"
/usr/bin/cp "$artifact" "$artifact_copy"
(
	cd "$artifact_dir"
	/usr/bin/sha256sum "$(basename "$artifact_copy")" > "$(basename "$artifact_copy").sha256"
)
artifact_sha256=$(/usr/bin/sha256sum "$artifact_copy" | /usr/bin/awk '{print $1}')
apk_tool="$sdk/staging_dir/host/bin/apk"
run_package_command "$apk_tool" adbdump "$artifact_copy" > "$artifact_copy.info.txt"
/usr/bin/awk 'found { print } /^paths:/ { found = 1; print }' "$artifact_copy.info.txt" > "$artifact_copy.files.txt"
run_package_command "$node_program" "$source_dir/scripts/openwrt-apk-metadata.mjs" \
	--adbdump "$artifact_copy.info.txt" --source-root "$source_dir" \
	--stage-manifest "$stage_dir/STAGE_MANIFEST.json" --package-tool-proof "$package_host_tool_proof" \
	--apk-sha256 "$artifact_sha256" --out "$artifact_dir/APK_METADATA_PROOF.json"
readonly apk_rootfs="$temporary_root/apk-rootfs"
mkdir -m 0700 "$apk_rootfs"
run_package_command "$apk_tool" --allow-untrusted extract --destination "$apk_rootfs" --no-chown "$artifact_copy"
run_package_command "$node_program" "$source_dir/scripts/openwrt-apk-rootfs.mjs" \
	--stage-manifest "$stage_dir/STAGE_MANIFEST.json" --rootfs "$apk_rootfs" \
	--apk-sha256 "$artifact_sha256" --out "$artifact_dir/APK_ROOTFS_PROOF.json"
cp "$stage_dir/payload/usr/lib/solovey-ui/SOURCE_FINGERPRINT.json" "$artifact_dir/SOURCE_FINGERPRINT.json"
cp "$stage_dir/STAGE_MANIFEST.json" "$artifact_dir/STAGE_MANIFEST.json"

echo "[openwrt-package-build] artifact: $artifact_copy"
