#!/usr/bin/env bash

set -Eeuo pipefail

APP_NAME="${SOLOVEY_UI_APP_NAME:-solovey-ui}"

target=""
binary="${APP_NAME}"
helper="solovey-protect-helper"
broker="solovey-privileged-broker"
proof="solovey-ssh-proof"
manifest_writer="solovey-broker-manifest"
manager="${APP_NAME}.sh"
service="${APP_NAME}.service"
systemd_dir="deploy/systemd"
build_info="BUILD_INFO.txt"
out_dir="dist/release"
package_tmp_dir=""
profile="full"

usage() {
    cat <<EOF
Package a Solovey UI Linux release artifact.

Usage:
  scripts/release-package-linux.sh --target linux-amd64 [options]

Options:
  --target <name>       Artifact target suffix, for example linux-amd64.
  --binary <path>       Built binary path. Default: ./${APP_NAME}
  --helper <path>       Restricted helper path. Default: ./solovey-protect-helper
  --broker <path>       Typed privileged broker. Default: ./solovey-privileged-broker
  --proof <path>        SSH proof client. Default: ./solovey-ssh-proof
  --manifest-writer <path> Fixed broker-manifest writer. Default: ./solovey-broker-manifest
  --manager <path>      Manager script path. Default: ./${APP_NAME}.sh
  --service <path>      systemd service path. Default: ./${APP_NAME}.service
  --systemd-dir <path>  Versioned profile unit directory. Default: ./deploy/systemd
  --build-info <path>   BUILD_INFO.txt path. Default: ./BUILD_INFO.txt
  --profile <full|core> Artifact profile. Default: full.
  --out-dir <path>      Output directory. Default: ./dist/release
  --help, -h            Show this help.
EOF
}

fail() {
    printf '[package-linux] ERROR: %s\n' "$*" >&2
    exit 1
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --target)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                target="$2"
                shift 2
                ;;
            --binary)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                binary="$2"
                shift 2
                ;;
            --helper)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                helper="$2"
                shift 2
                ;;
            --manager)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                manager="$2"
                shift 2
                ;;
            --broker)
				[[ $# -ge 2 ]] || fail "$1 requires a value"
				broker="$2"
				shift 2
				;;
            --proof)
				[[ $# -ge 2 ]] || fail "$1 requires a value"
				proof="$2"
				shift 2
				;;
            --manifest-writer)
				[[ $# -ge 2 ]] || fail "$1 requires a value"
				manifest_writer="$2"
				shift 2
				;;
            --service)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                service="$2"
                shift 2
                ;;
            --systemd-dir)
				[[ $# -ge 2 ]] || fail "$1 requires a value"
				systemd_dir="$2"
				shift 2
				;;
            --build-info)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                build_info="$2"
                shift 2
                ;;
            --profile)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                profile="$2"
                shift 2
                ;;
            --out-dir)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                out_dir="$2"
                shift 2
                ;;
            --help|-h)
                usage
                exit 0
                ;;
            *)
                fail "unknown option: $1"
                ;;
        esac
    done
}

require_file() {
    local label="$1"
    local path="$2"

    [[ -f "${path}" ]] || fail "${label} not found: ${path}"
}

require_executable() {
    local label="$1"
    local path="$2"

    [[ -x "${path}" ]] || fail "${label} not found or not executable: ${path}"
}

require_build_key() {
    local key="$1"

    grep -Eq "^${key}=.+$" "${build_info}" || fail "BUILD_INFO.txt misses ${key}"
}

validate_inputs() {
    [[ -n "${target}" ]] || fail "--target is required"
    case "${target}" in
        linux-*) ;;
        *) fail "--target must look like linux-<arch>: ${target}" ;;
    esac
    case "${profile}" in
        full|core) ;;
        *) fail "--profile must be full or core: ${profile}" ;;
    esac

    command -v tar >/dev/null 2>&1 || fail "tar is required"
    command -v sha256sum >/dev/null 2>&1 || fail "sha256sum is required"

    require_executable "binary" "${binary}"
	require_executable "privileged broker" "${broker}"
	require_executable "SSH proof client" "${proof}"
	require_executable "broker manifest writer" "${manifest_writer}"
    if [[ "${profile}" == "full" ]]; then
        require_executable "restricted helper" "${helper}"
    fi
    require_executable "manager script" "${manager}"
    require_file "systemd service" "${service}"
	[[ -d "${systemd_dir}" ]] || fail "systemd profile directory not found: ${systemd_dir}"
	for unit in solovey-ui-native-hardened.service solovey-ui-native-network-advanced.service solovey-ui-native-legacy-root.service \
		solovey-privileged-broker.service solovey-privileged-broker.socket solovey-privileged-proof.socket solovey-ui.sysusers solovey-ui.tmpfiles; do
		require_file "systemd asset ${unit}" "${systemd_dir}/${unit}"
	done
    require_file "build metadata" "${build_info}"

    grep -Eq "^app=${APP_NAME}$" "${build_info}" || fail "BUILD_INFO.txt app must be ${APP_NAME}"
    grep -Eq "^profile=${profile}$" "${build_info}" || fail "BUILD_INFO.txt profile must be ${profile}"
    require_build_key version
    require_build_key commit
    require_build_key platform
    require_build_key go
    require_build_key sing_box
}

package_release() {
    local tmp_dir payload artifact checksum

    tmp_dir="$(mktemp -d)"
    package_tmp_dir="${tmp_dir}"
    trap 'rm -rf "${package_tmp_dir:-}"' EXIT
    payload="${tmp_dir}/${APP_NAME}"
    if [[ "${profile}" == "core" ]]; then
        artifact="${APP_NAME}-core-${target}.tar.gz"
    else
        artifact="${APP_NAME}-${target}.tar.gz"
    fi
    checksum="${artifact}.sha256"

    mkdir -p "${payload}" "${out_dir}"
    cp -a "${binary}" "${payload}/${APP_NAME}"
	cp -a "${broker}" "${payload}/solovey-privileged-broker"
	cp -a "${proof}" "${payload}/solovey-ssh-proof"
	cp -a "${manifest_writer}" "${payload}/solovey-broker-manifest"
	mkdir -p "${payload}/systemd"
	cp -a "${systemd_dir}/." "${payload}/systemd/"
    if [[ "${profile}" == "full" ]]; then
        cp -a "${helper}" "${payload}/solovey-protect-helper"
    fi
    cp -a "${manager}" "${payload}/${APP_NAME}.sh"
    cp -a "${service}" "${payload}/${APP_NAME}.service"
    cp -a "${build_info}" "${payload}/BUILD_INFO.txt"
    chmod 755 "${payload}/${APP_NAME}" "${payload}/${APP_NAME}.sh" "${payload}/solovey-privileged-broker" \
		"${payload}/solovey-ssh-proof" "${payload}/solovey-broker-manifest"
    if [[ "${profile}" == "full" ]]; then
        chmod 755 "${payload}/solovey-protect-helper"
    fi

    tar -czf "${out_dir}/${artifact}" -C "${tmp_dir}" "${APP_NAME}"
    (
        cd "${out_dir}"
        sha256sum "${artifact}" > "${checksum}"
    )

    printf '%s\n' "${out_dir}/${artifact}"
    printf '%s\n' "${out_dir}/${checksum}"
}

parse_args "$@"
validate_inputs
package_release
