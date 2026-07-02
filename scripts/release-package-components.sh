#!/usr/bin/env bash

set -Eeuo pipefail

APP_NAME="${SOLOVEY_UI_APP_NAME:-solovey-ui}"

components_dir="components"
out_dir="dist/release"
artifact="${APP_NAME}-components.tar.gz"
package_tmp_dir=""

usage() {
    cat <<EOF
Package Solovey UI component packs.

Usage:
  scripts/release-package-components.sh [options]

Options:
  --components-dir <path> Source component directory. Default: ./components
  --out-dir <path>        Output directory. Default: ./dist/release
  --artifact <name>       Artifact file name. Default: ${APP_NAME}-components.tar.gz
  --help, -h              Show this help.
EOF
}

fail() {
    printf '[package-components] ERROR: %s\n' "$*" >&2
    exit 1
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --components-dir)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                components_dir="$2"
                shift 2
                ;;
            --out-dir)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                out_dir="$2"
                shift 2
                ;;
            --artifact)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                artifact="$2"
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

validate_inputs() {
    [[ -d "${components_dir}" ]] || fail "components directory not found: ${components_dir}"
    [[ "${artifact}" == *.tar.gz ]] || fail "--artifact must end with .tar.gz: ${artifact}"
    command -v tar >/dev/null 2>&1 || fail "tar is required"
    command -v sha256sum >/dev/null 2>&1 || fail "sha256sum is required"
}

copy_optional_dir() {
    local component_source="$1"
    local component_target="$2"
    local name="$3"

    if [[ -d "${component_source}/${name}" ]]; then
        cp -a "${component_source}/${name}" "${component_target}/${name}"
    fi
}

package_components() {
    local tmp_dir payload checksum component_source component_id component_target found

    tmp_dir="$(mktemp -d)"
    package_tmp_dir="${tmp_dir}"
    trap 'rm -rf "${package_tmp_dir:-}"' EXIT

    payload="${tmp_dir}/components"
    mkdir -p "${payload}" "${out_dir}"

    found=0
    while IFS= read -r -d '' component_source; do
        component_id="$(basename "${component_source}")"
        [[ -f "${component_source}/component.json" ]] || continue

        found=1
        component_target="${payload}/${component_id}"
        mkdir -p "${component_target}"
        cp -a "${component_source}/component.json" "${component_target}/component.json"

        # These directories are runtime pack payloads. Go sources and tests stay
        # in the repository and must not be shipped as install footprint.
        copy_optional_dir "${component_source}" "${component_target}" "frontend"
        copy_optional_dir "${component_source}" "${component_target}" "migrations"
        copy_optional_dir "${component_source}" "${component_target}" "settings"
        copy_optional_dir "${component_source}" "${component_target}" "systemd"
        copy_optional_dir "${component_source}" "${component_target}" "bin"
    done < <(find "${components_dir}" -mindepth 1 -maxdepth 1 -type d -print0 | sort -z)

    [[ "${found}" == "1" ]] || fail "no component packs found in ${components_dir}"

    tar -czf "${out_dir}/${artifact}" -C "${tmp_dir}" components
    checksum="${artifact}.sha256"
    (
        cd "${out_dir}"
        sha256sum "${artifact}" > "${checksum}"
    )

    printf '%s\n' "${out_dir}/${artifact}"
    printf '%s\n' "${out_dir}/${checksum}"
}

parse_args "$@"
validate_inputs
package_components
