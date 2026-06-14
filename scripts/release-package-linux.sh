#!/usr/bin/env bash

set -Eeuo pipefail

APP_NAME="${SOLOVEY_UI_APP_NAME:-solovey-ui}"

target=""
binary="${APP_NAME}"
manager="${APP_NAME}.sh"
service="${APP_NAME}.service"
build_info="BUILD_INFO.txt"
out_dir="dist/release"
package_tmp_dir=""

usage() {
    cat <<EOF
Package a Solovey UI Linux release artifact.

Usage:
  scripts/release-package-linux.sh --target linux-amd64 [options]

Options:
  --target <name>       Artifact target suffix, for example linux-amd64.
  --binary <path>       Built binary path. Default: ./${APP_NAME}
  --manager <path>      Manager script path. Default: ./${APP_NAME}.sh
  --service <path>      systemd service path. Default: ./${APP_NAME}.service
  --build-info <path>   BUILD_INFO.txt path. Default: ./BUILD_INFO.txt
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
            --manager)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                manager="$2"
                shift 2
                ;;
            --service)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                service="$2"
                shift 2
                ;;
            --build-info)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                build_info="$2"
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

    command -v tar >/dev/null 2>&1 || fail "tar is required"
    command -v sha256sum >/dev/null 2>&1 || fail "sha256sum is required"

    require_executable "binary" "${binary}"
    require_executable "manager script" "${manager}"
    require_file "systemd service" "${service}"
    require_file "build metadata" "${build_info}"

    grep -Eq "^app=${APP_NAME}$" "${build_info}" || fail "BUILD_INFO.txt app must be ${APP_NAME}"
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
    artifact="${APP_NAME}-${target}.tar.gz"
    checksum="${artifact}.sha256"

    mkdir -p "${payload}" "${out_dir}"
    cp -a "${binary}" "${payload}/${APP_NAME}"
    cp -a "${manager}" "${payload}/${APP_NAME}.sh"
    cp -a "${service}" "${payload}/${APP_NAME}.service"
    cp -a "${build_info}" "${payload}/BUILD_INFO.txt"
    chmod 755 "${payload}/${APP_NAME}" "${payload}/${APP_NAME}.sh"

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
