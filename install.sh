#!/usr/bin/env bash

set -Eeuo pipefail

APP_NAME="solovey-ui"
SERVICE_NAME="solovey-ui"
REPO="${SOLOVEY_UI_REPO:-MalenkiySolovey/solovey-ui}"

INSTALL_DIR="${SOLOVEY_UI_INSTALL_DIR:-/usr/local/${APP_NAME}}"
RELEASES_DIR="${INSTALL_DIR}/releases"
CURRENT_RELEASE_DIR="${RELEASES_DIR}/current"
BIN_PATH="${CURRENT_RELEASE_DIR}/${APP_NAME}"
MANAGER_PATH="${CURRENT_RELEASE_DIR}/${APP_NAME}.sh"
CLI_PATH="${SOLOVEY_UI_CLI_PATH:-/usr/bin/${APP_NAME}}"
SYSTEMD_SERVICE="${SOLOVEY_UI_SYSTEMD_SERVICE:-/etc/systemd/system/${SERVICE_NAME}.service}"
SYSTEMD_UNIT_ROOT="${SOLOVEY_UI_SYSTEMD_UNIT_ROOT:-${SYSTEMD_SERVICE%/*}}"
SYSTEMD_PROFILE_ROOT="${SOLOVEY_UI_SYSTEMD_PROFILE_ROOT:-/usr/local/lib/${APP_NAME}/systemd}"
DEPLOYMENT_MARKER="${SOLOVEY_UI_DEPLOYMENT_MARKER:-/etc/${APP_NAME}/deployment-profile}"
HARDENED_DATA_ROOT="${SOLOVEY_UI_HARDENED_DATA_ROOT:-/var/lib/${APP_NAME}}"
ENV_DIR="${SOLOVEY_UI_ENV_DIR:-/etc/${APP_NAME}}"
SECRETBOX_ENV_FILE="${SOLOVEY_UI_SECRETBOX_ENV_FILE:-${ENV_DIR}/secretbox.env}"
BACKUP_ROOT="${SOLOVEY_UI_BACKUP_ROOT:-/var/backups/${APP_NAME}}"

GITHUB_API="${SOLOVEY_UI_GITHUB_API:-https://api.github.com/repos/${REPO}/releases/latest}"
GITHUB_RELEASES="${SOLOVEY_UI_GITHUB_RELEASES:-https://github.com/${REPO}/releases/download}"
LEGACY_SERVICE_NAME="${SOLOVEY_UI_LEGACY_SERVICE_NAME:-s-ui}"
LEGACY_DIR="${SOLOVEY_UI_LEGACY_DIR:-/usr/local/s-ui}"
LEGACY_ENV_DIR="${SOLOVEY_UI_LEGACY_ENV_DIR:-/etc/s-ui}"
LEGACY_SECRETBOX_ENV_FILE="${SOLOVEY_UI_LEGACY_SECRETBOX_ENV_FILE:-${LEGACY_ENV_DIR}/secretbox.env}"
LEGACY_SERVICE_FILE="${SOLOVEY_UI_LEGACY_SERVICE_FILE:-/etc/systemd/system/${LEGACY_SERVICE_NAME}.service}"
LEGACY_DROPIN_DIR="${SOLOVEY_UI_LEGACY_DROPIN_DIR:-/etc/systemd/system/${LEGACY_SERVICE_NAME}.service.d}"
LEGACY_DB="${SOLOVEY_UI_LEGACY_DB:-${LEGACY_DIR}/db/s-ui.db}"
LEGACY_CERT_DIR="${SOLOVEY_UI_LEGACY_CERT_DIR:-${LEGACY_DIR}/cert}"
TARGET_DB="${INSTALL_DIR}/db/${APP_NAME}.db"
DEPLOYMENT_PROFILE=""

DRY_RUN=0
NON_INTERACTIVE=0
BACKUP_MODE="auto"
MIGRATE_FROM_SUI=0
FORCE_MIGRATE=0
REQUESTED_PROFILE="${SOLOVEY_UI_PROFILE:-full}"
BINARY_PROFILE=""
REQUIRE_CORE=0
VERSION=""
BACKUP_PATH=""
DOWNLOAD_TMP_DIR=""
COMPONENT_PAYLOAD_DIR=""
CURL_CONNECT_TIMEOUT="${SOLOVEY_UI_CURL_CONNECT_TIMEOUT:-20}"
CURL_MAX_TIME="${SOLOVEY_UI_CURL_MAX_TIME:-300}"
COMPONENT_IDS_RAW="${SOLOVEY_UI_COMPONENT_IDS:-}"
COMPONENT_IDS=()
COMPONENT_IDS_LOADED=0
WITH_COMPONENTS=()
WITHOUT_COMPONENTS=()

usage() {
    cat <<EOF
Solovey UI installer

Usage:
  bash install.sh [options] [version]

Options:
  --version, --tag <tag>  Install a specific release tag.
  --dry-run              Print planned operations without changing the system.
  --non-interactive, -y   Disable prompts. Currently the installer is prompt-free.
  --backup               Always create a backup before installing.
  --no-backup            Skip backup creation.
  --profile <full|minimal|core>
                         Component footprint profile. Default: full.
                         "core" is accepted as an alias for "minimal".
                         The installer resolves the minimal sufficient binary
                         profile separately from this footprint profile.
  --require-core         Fail if selected components require the full binary.
  --with <ids>           Install only these optional components when provided.
                         Accepts comma-separated ids or "all"; repeatable.
  --without <ids>        Disable these optional components. Accepts comma-separated
                         ids or "all"; repeatable.
  --migrate-from-sui     Copy a legacy /usr/local/s-ui install into Solovey UI.
  --force-migrate        Allow --migrate-from-sui to replace an existing new DB.
  --help, -h             Show this help.

Examples:
  bash install.sh
  bash install.sh --version v2026.2.0
  bash install.sh --without component-id,another-component
  bash install.sh --with component-id
  bash install.sh --dry-run
  bash install.sh --migrate-from-sui
EOF
}

log() {
    printf '[%s] %s\n' "${APP_NAME}" "$*"
}

warn() {
    printf '[%s] WARNING: %s\n' "${APP_NAME}" "$*" >&2
}

fail() {
    printf '[%s] ERROR: %s\n' "${APP_NAME}" "$*" >&2
    exit 1
}

run() {
    if [[ "${DRY_RUN}" == "1" ]]; then
        printf '[%s] DRY RUN:' "${APP_NAME}"
        printf ' %q' "$@"
        printf '\n'
        return 0
    fi
    "$@"
}

component_id_valid() {
    [[ "$1" =~ ^[a-z0-9-]+$ ]]
}

append_component_list() {
    local target="$1"
    local raw="$2"
    local old_ifs item

    old_ifs="${IFS}"
    IFS=','
    for item in ${raw}; do
		item="${item//[[:space:]]/}"
		[[ -n "${item}" ]] || continue
		if [[ "${item}" != "all" ]]; then
			component_id_valid "${item}" || fail "invalid component id: ${item}"
		fi
        if [[ "${target}" == "with" ]]; then
            WITH_COMPONENTS+=("${item}")
        else
            WITHOUT_COMPONENTS+=("${item}")
        fi
    done
    IFS="${old_ifs}"
}

load_component_ids_from_payload() {
    local dir="$1"
    local source manifest id seen

    COMPONENT_IDS=()
    [[ -d "${dir}" ]] || fail "component payload directory is missing: ${dir}"
    for source in "${dir}"/*; do
        [[ -d "${source}" ]] || continue
        manifest="${source}/component.json"
        [[ -f "${manifest}" ]] || continue
        id="$(basename "${source}")"
        component_id_valid "${id}" || fail "invalid component id in pack manifest: ${manifest}"
        validate_component_pack_manifest "${id}" "${manifest}"
        for seen in "${COMPONENT_IDS[@]}"; do
            [[ "${seen}" == "${id}" ]] && fail "duplicate component pack id: ${id}"
        done
        COMPONENT_IDS+=("${id}")
    done
    COMPONENT_IDS_LOADED=1
    validate_component_filters
}

component_ids_loaded() {
    [[ "${COMPONENT_IDS_LOADED}" == "1" ]]
}

component_known() {
    local id="$1"
    local known
    component_ids_loaded || fail "component catalog is not loaded"
    for known in "${COMPONENT_IDS[@]}"; do
        [[ "${known}" == "${id}" ]] && return 0
    done
    return 1
}

validate_component_filters() {
    local item
    component_ids_loaded || return 0
    for item in "${WITH_COMPONENTS[@]}" "${WITHOUT_COMPONENTS[@]}"; do
        [[ -n "${item}" ]] || continue
        [[ "${item}" == "all" ]] && continue
        component_known "${item}" || fail "unknown component id: ${item}"
    done
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --version|--tag)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                VERSION="$2"
                shift 2
                ;;
            --dry-run)
                DRY_RUN=1
                shift
                ;;
            --non-interactive|-y)
                NON_INTERACTIVE=1
                shift
                ;;
            --backup)
                BACKUP_MODE="always"
                shift
                ;;
            --no-backup)
                BACKUP_MODE="never"
                shift
                ;;
            --profile)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                REQUESTED_PROFILE="$2"
                shift 2
                ;;
            --require-core)
                REQUIRE_CORE=1
                shift
                ;;
            --with)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                append_component_list "with" "$2"
                shift 2
                ;;
            --without)
                [[ $# -ge 2 ]] || fail "$1 requires a value"
                append_component_list "without" "$2"
                shift 2
                ;;
            --migrate-from-sui)
                MIGRATE_FROM_SUI=1
                shift
                ;;
            --force-migrate)
                FORCE_MIGRATE=1
                shift
                ;;
            --help|-h)
                usage
                exit 0
                ;;
            -*)
                fail "unknown option: $1"
                ;;
            *)
                [[ -z "${VERSION}" ]] || fail "multiple versions provided"
                VERSION="$1"
                shift
                ;;
        esac
    done

    normalize_requested_profile
    resolve_binary_profile
}

require_root() {
    if [[ "${SOLOVEY_UI_ALLOW_NON_ROOT:-0}" == "1" ]]; then
        return 0
    fi
    [[ "${EUID}" -eq 0 ]] || fail "run as root, for example: sudo bash install.sh"
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

component_in_list() {
    local needle="$1"
    shift
    local item
    for item in "$@"; do
        [[ "${item}" == "${needle}" ]] && return 0
    done
    return 1
}

component_list_has_all() {
    component_in_list "all" "$@"
}

component_selection_needs_bundle() {
    if [[ "${#WITH_COMPONENTS[@]}" -gt 0 ]]; then
        component_list_has_all "${WITHOUT_COMPONENTS[@]}" && return 1
        return 0
    fi
    if [[ "${REQUESTED_PROFILE}" == "minimal" ]]; then
        return 1
    fi
    component_list_has_all "${WITHOUT_COMPONENTS[@]}" && return 1
    return 0
}

component_installed() {
    local id="$1"

    if [[ "${#WITH_COMPONENTS[@]}" -gt 0 ]] && ! component_list_has_all "${WITH_COMPONENTS[@]}" && ! component_in_list "${id}" "${WITH_COMPONENTS[@]}"; then
        return 1
    fi
    if [[ "${REQUESTED_PROFILE}" == "minimal" && "${#WITH_COMPONENTS[@]}" -eq 0 ]]; then
        return 1
    fi
    if component_list_has_all "${WITHOUT_COMPONENTS[@]}" || component_in_list "${id}" "${WITHOUT_COMPONENTS[@]}"; then
        return 1
    fi
    return 0
}

any_component_installed() {
    local id
    if ! component_ids_loaded; then
        component_selection_needs_bundle
        return
    fi
    for id in "${COMPONENT_IDS[@]}"; do
        if component_installed "${id}"; then
            return 0
        fi
    done
    return 1
}

all_components_installed() {
    local id
    if ! component_ids_loaded; then
        [[ "${REQUESTED_PROFILE}" == "full" && "${#WITH_COMPONENTS[@]}" -eq 0 ]] && ! component_list_has_all "${WITHOUT_COMPONENTS[@]}"
        return
    fi
    for id in "${COMPONENT_IDS[@]}"; do
        component_installed "${id}" || return 1
    done
    return 0
}

normalize_requested_profile() {
    case "${REQUESTED_PROFILE}" in
        full|minimal) ;;
        core) REQUESTED_PROFILE="minimal" ;;
        *) fail "--profile must be full, minimal or core: ${REQUESTED_PROFILE}" ;;
    esac
}

resolve_binary_profile() {
    if any_component_installed; then
        # All first-wave backend components are in-process. Sidecar-capable
        # components can lower this decision once their delivery is real.
        BINARY_PROFILE="full"
    else
        BINARY_PROFILE="core"
    fi

    if [[ "${REQUIRE_CORE}" == "1" && "${BINARY_PROFILE}" != "core" ]]; then
        fail "selected components require the full binary; remove in-process components or omit --require-core"
    fi
}

component_profile_name() {
    if ! any_component_installed; then
        printf 'core\n'
        return
    fi
    if all_components_installed; then
        printf 'full\n'
    else
        printf 'custom\n'
    fi
}

component_summary() {
    local id enabled enabled_ids=() disabled_ids=()
    if ! component_ids_loaded; then
        if any_component_installed; then
            printf 'resolved-from-component-bundle'
        else
            printf 'enabled=none'
        fi
        if [[ "${#WITH_COMPONENTS[@]}" -gt 0 ]]; then
            printf ' with=%s' "$(IFS=,; printf '%s' "${WITH_COMPONENTS[*]}")"
        fi
        if [[ "${#WITHOUT_COMPONENTS[@]}" -gt 0 ]]; then
            printf ' without=%s' "$(IFS=,; printf '%s' "${WITHOUT_COMPONENTS[*]}")"
        fi
        return
    fi
    for id in "${COMPONENT_IDS[@]}"; do
        if component_installed "${id}"; then
            enabled_ids+=("${id}")
        else
            disabled_ids+=("${id}")
        fi
    done
    enabled="$(IFS=,; printf '%s' "${enabled_ids[*]:-none}")"
    printf 'enabled=%s' "${enabled:-none}"
    if [[ "${#disabled_ids[@]}" -gt 0 ]]; then
        printf ' disabled=%s' "$(IFS=,; printf '%s' "${disabled_ids[*]}")"
    fi
}

release_artifact_name() {
    local platform="$1"
    if [[ "${BINARY_PROFILE}" == "core" ]]; then
        printf '%s-core-linux-%s.tar.gz\n' "${APP_NAME}" "${platform}"
    else
        printf '%s-linux-%s.tar.gz\n' "${APP_NAME}" "${platform}"
    fi
}

components_bundle_artifact_name() {
    printf '%s-components.tar.gz\n' "${APP_NAME}"
}

component_manifest_string_field() {
    local manifest="$1"
    local field="$2"
    sed -nE "s/.*\"${field}\"[[:space:]]*:[[:space:]]*\"([^\"]+)\".*/\\1/p" "${manifest}" | head -n 1
}

validate_component_pack_manifest() {
    local id="$1"
    local manifest="$2"
    local manifest_id delivery

    manifest_id="$(component_manifest_string_field "${manifest}" "id")"
    delivery="$(component_manifest_string_field "${manifest}" "delivery")"
    [[ "${manifest_id}" == "${id}" ]] || fail "component pack id mismatch: expected ${id}, got ${manifest_id:-<missing>}"
	[[ "${delivery}" == "in-process" ]] || fail "component pack ${id} has unsupported delivery: ${delivery:-<missing>}"
}

validate_component_payload() {
    local id source

    any_component_installed || return 0
    [[ -n "${COMPONENT_PAYLOAD_DIR}" ]] || fail "component payload directory is not prepared"
    [[ -d "${COMPONENT_PAYLOAD_DIR}" ]] || fail "component payload directory is missing: ${COMPONENT_PAYLOAD_DIR}"

    for id in "${COMPONENT_IDS[@]}"; do
        component_installed "${id}" || continue
        source="${COMPONENT_PAYLOAD_DIR}/${id}"
        [[ -d "${source}" ]] || fail "component pack is missing from bundle: ${id}"
        [[ -f "${source}/component.json" ]] || fail "component pack misses component.json: ${id}"
        validate_component_pack_manifest "${id}" "${source}/component.json"
    done
}

validate_tar_paths() {
    local archive="$1"
    local member part
    local -a parts

    while IFS= read -r member; do
        [[ -n "${member}" ]] || continue
        [[ "${member}" != /* ]] || fail "archive contains absolute path: ${member}"
        [[ "${member}" != *\\* ]] || fail "archive contains backslash path: ${member}"
        IFS='/' read -r -a parts <<< "${member}"
        for part in "${parts[@]}"; do
            [[ "${part}" != ".." ]] || fail "archive contains parent traversal: ${member}"
        done
    done < <(tar -tzf "${archive}")
}

safe_extract_tar() {
    local archive="$1"
    local target="$2"

    validate_tar_paths "${archive}"
    tar -xzf "${archive}" -C "${target}"
}

write_component_metadata() {
    local dir="${INSTALL_DIR}/components"
    local path="${dir}/installed.json"
    local tmp="${path}.tmp.$$"
    local id comma index total_installed

    if [[ "${DRY_RUN}" == "1" ]]; then
        log "would write component metadata: profile=$(component_profile_name) binary=${BINARY_PROFILE} $(component_summary)"
        return 0
    fi

    mkdir -p "${dir}" || return
    {
        printf '{\n'
        printf '  "version": 1,\n'
        printf '  "profile": "%s",\n' "$(component_profile_name)"
        printf '  "binary": "%s",\n' "${BINARY_PROFILE}"
        printf '  "components": [\n'
        index=0
        total_installed=0
        for id in "${COMPONENT_IDS[@]}"; do
            component_installed "${id}" && total_installed=$((total_installed + 1))
        done
        for id in "${COMPONENT_IDS[@]}"; do
            component_installed "${id}" || continue
            index=$((index + 1))
            comma=","
            [[ "${index}" -eq "${total_installed}" ]] && comma=""
            printf '    {"id": "%s", "delivery": "in-process", "installed": true}%s\n' "${id}" "${comma}"
        done
        printf '  ]\n'
        printf '}\n'
    } > "${tmp}" || return
    chmod 600 "${tmp}" || return
    mv "${tmp}" "${path}"
}

install_component_pack_dir() {
    local id="$1"
    local source="${COMPONENT_PAYLOAD_DIR}/${id}"
    local target="${INSTALL_DIR}/components/${id}"
    local incoming="${target}.incoming.$$"
    local previous="${target}.previous.$$"

    [[ -n "${COMPONENT_PAYLOAD_DIR}" ]] || fail "component payload directory is not prepared"
    [[ -d "${source}" ]] || fail "component pack is missing from bundle: ${id}"
    [[ -f "${source}/component.json" ]] || fail "component pack misses component.json: ${id}"
    validate_component_pack_manifest "${id}" "${source}/component.json"

    rm -rf "${incoming}" "${previous}"
    cp -a "${source}" "${incoming}" || { rm -rf "${incoming}"; return 1; }
    if [[ -e "${target}" ]]; then
        mv "${target}" "${previous}" || { rm -rf "${incoming}"; return 1; }
    fi
    if ! mv "${incoming}" "${target}"; then
        if [[ -e "${previous}" && ! -e "${target}" ]]; then
            mv "${previous}" "${target}" || true
        fi
        rm -rf "${incoming}"
        return 1
    fi
    rm -rf "${previous}"
}

install_component_packs() {
    local id existing existing_id

    if [[ "${DRY_RUN}" == "1" ]]; then
        log "would install component packs: $(component_summary)"
        return 0
    fi

    mkdir -p "${INSTALL_DIR}/components" || return

    for id in "${COMPONENT_IDS[@]}"; do
        if component_installed "${id}"; then
            install_component_pack_dir "${id}" || return
        else
            rm -rf "${INSTALL_DIR}/components/${id}" \
                "${INSTALL_DIR}/components/${id}.incoming."* \
                "${INSTALL_DIR}/components/${id}.previous."*
        fi
    done

    for existing in "${INSTALL_DIR}/components"/*; do
        [[ -d "${existing}" ]] || continue
        existing_id="$(basename "${existing}")"
        [[ "${existing_id}" != "installed.json" ]] || continue
        if ! component_known "${existing_id}" || ! component_installed "${existing_id}"; then
            rm -rf "${existing}" \
                "${INSTALL_DIR}/components/${existing_id}.incoming."* \
                "${INSTALL_DIR}/components/${existing_id}.previous."*
        fi
    done
}

secure_curl() {
    curl --fail --location --silent --show-error \
        --proto '=https' --tlsv1.2 \
        --connect-timeout "${CURL_CONNECT_TIMEOUT}" \
        --max-time "${CURL_MAX_TIME}" \
        --retry 3 --retry-delay 2 --retry-all-errors \
        "$@"
}

require_tools() {
    require_command uname
    require_command curl
    require_command sed
    require_command grep

    if [[ "${DRY_RUN}" != "1" ]]; then
        require_command tar
        require_command sha256sum
        require_command systemctl
		require_command systemd-sysusers
		require_command systemd-tmpfiles
		require_command runuser
        require_command base64
        require_command dd
        if [[ "${MIGRATE_FROM_SUI}" == "1" ]]; then
            require_command sqlite3
        fi
    fi
}

detect_arch() {
    local machine
    machine="$(uname -m)"
    case "${machine}" in
        x86_64|amd64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        armv7l|armv7*) echo "armv7" ;;
        armv6l|armv6*) echo "armv6" ;;
        armv5tel|armv5*) echo "armv5" ;;
        i386|i686) echo "386" ;;
        s390x) echo "s390x" ;;
        *) fail "unsupported architecture: ${machine}" ;;
    esac
}

latest_version() {
    local tag
    tag="$(
        secure_curl \
            -H "Accept: application/vnd.github+json" \
            -H "User-Agent: ${APP_NAME}-installer" \
            "${GITHUB_API}" |
        sed -nE 's/^[[:space:]]*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' |
        head -n 1
    )"
    [[ -n "${tag}" ]] || fail "could not resolve latest release from ${GITHUB_API}"
    printf '%s\n' "${tag}"
}

maybe_warn_legacy_install() {
    if [[ -d "${LEGACY_DIR}" ]]; then
        if [[ "${MIGRATE_FROM_SUI}" == "1" ]]; then
            log "legacy s-ui install detected at ${LEGACY_DIR}; migration is enabled"
            return 0
        fi
        warn "legacy s-ui install detected at ${LEGACY_DIR}; run with --migrate-from-sui to migrate it"
        if [[ -f "${LEGACY_DB}" ]]; then
            warn "legacy database found at ${LEGACY_DB}; keep it backed up before any manual migration"
        fi
    fi
}

describe_legacy_migration_plan() {
    [[ "${MIGRATE_FROM_SUI}" == "1" ]] || return 0

    log "legacy migration plan:"
    log "  legacy DB: ${LEGACY_DB}"
    log "  target DB: ${TARGET_DB}"
    log "  legacy env: ${LEGACY_SECRETBOX_ENV_FILE}"
    log "  target env: ${SECRETBOX_ENV_FILE}"
    log "  legacy cert dir: ${LEGACY_CERT_DIR}"
    log "  target cert dir: ${INSTALL_DIR}/cert"
    log "  legacy service: ${LEGACY_SERVICE_NAME}"
}

validate_legacy_migration_ready() {
    [[ "${MIGRATE_FROM_SUI}" == "1" ]] || return 0

    [[ -f "${LEGACY_DB}" ]] || fail "--migrate-from-sui requested, but legacy DB does not exist: ${LEGACY_DB}"

    if [[ "${FORCE_MIGRATE}" != "1" ]]; then
        if [[ -f "${TARGET_DB}" || -f "${TARGET_DB}-wal" || -f "${TARGET_DB}-shm" ]]; then
            fail "target DB already exists: ${TARGET_DB}; rerun with --force-migrate only after checking the backup"
        fi
    fi
}

backup_path_size_kb() {
    local path="$1"
    local size

    if [[ ! -e "${path}" ]]; then
        printf '0\n'
        return 0
    fi

    size="$(du -sk "${path}" 2>/dev/null | awk 'NR == 1 { print $1 }')"
    [[ -n "${size}" ]] || fail "cannot estimate backup size for ${path}"
    printf '%s\n' "${size}"
}

estimate_backup_size_kb() {
    local total=0
    local size

    size="$(backup_path_size_kb "${INSTALL_DIR}")"
    total=$((total + size))
    size="$(backup_path_size_kb "${ENV_DIR}")"
    total=$((total + size))
    size="$(backup_path_size_kb "${SYSTEMD_SERVICE}")"
    total=$((total + size))
	size="$(backup_path_size_kb "${HARDENED_DATA_ROOT}")"
	total=$((total + size))
	size="$(backup_path_size_kb "${SYSTEMD_PROFILE_ROOT}")"
	total=$((total + size))
	for unit in solovey-privileged-broker.service solovey-privileged-broker.socket solovey-privileged-proof.socket; do
		size="$(backup_path_size_kb "${SYSTEMD_UNIT_ROOT}/${unit}")"
		total=$((total + size))
	done

    if [[ "${MIGRATE_FROM_SUI}" == "1" ]]; then
        size="$(backup_path_size_kb "${LEGACY_DIR}")"
        total=$((total + size))
        size="$(backup_path_size_kb "${LEGACY_ENV_DIR}")"
        total=$((total + size))
        size="$(backup_path_size_kb "${LEGACY_SERVICE_FILE}")"
        total=$((total + size))
        size="$(backup_path_size_kb "${LEGACY_DROPIN_DIR}")"
        total=$((total + size))
    fi

    printf '%s\n' "${total}"
}

ensure_backup_space() {
    local required_kb needed_kb available_kb

    [[ "${DRY_RUN}" != "1" ]] || return 0

    mkdir -p "${BACKUP_ROOT}"
    required_kb="$(estimate_backup_size_kb)"
    if [[ "${required_kb}" -le 0 ]]; then
        return 0
    fi

    needed_kb=$(((required_kb * 12 + 9) / 10))
    available_kb="$(df -k "${BACKUP_ROOT}" 2>/dev/null | awk 'NR == 2 { print $4 }')"
    [[ -n "${available_kb}" ]] || fail "cannot determine free space for backup root: ${BACKUP_ROOT}"

    if [[ "${available_kb}" -lt "${needed_kb}" ]]; then
        fail "not enough disk space for backup: need about ${needed_kb} KiB, available ${available_kb} KiB at ${BACKUP_ROOT}"
    fi
}

cleanup_incomplete_backup() {
    local target="$1"

    if [[ "${DRY_RUN}" != "1" && -n "${target}" && -d "${target}" ]]; then
        rm -rf "${target}"
    fi
}

backup_systemd_assets() {
	local target="$1" unit state
	mkdir -p "${target}/systemd-assets/units" || return 1
	if [[ -d "${SYSTEMD_PROFILE_ROOT}" ]]; then
		copy_backup_path "${SYSTEMD_PROFILE_ROOT}" "${target}/systemd-assets/profiles" "${target}" || return 1
		printf 'profiles=present\n' >> "${target}/systemd-assets/inventory.txt"
	else
		printf 'profiles=absent\n' >> "${target}/systemd-assets/inventory.txt"
	fi
	for unit in solovey-privileged-broker.service solovey-privileged-broker.socket solovey-privileged-proof.socket; do
		state=absent
		if [[ -f "${SYSTEMD_UNIT_ROOT}/${unit}" ]]; then
			copy_backup_path "${SYSTEMD_UNIT_ROOT}/${unit}" "${target}/systemd-assets/units/${unit}" "${target}" || return 1
			state=present
		fi
		printf '%s=%s\n' "${unit}" "${state}" >> "${target}/systemd-assets/inventory.txt"
	done
}

copy_backup_path() {
    local source="$1"
    local target="$2"
    local backup_dir="$3"

    if ! run cp -a "${source}" "${target}"; then
        cleanup_incomplete_backup "${backup_dir}"
        fail "backup failed while copying ${source}; removed incomplete backup ${backup_dir}"
    fi
}

backup_existing() {
    local should_backup=0

    case "${BACKUP_MODE}" in
        always) should_backup=1 ;;
        never) should_backup=0 ;;
        auto)
            if [[ -d "${INSTALL_DIR}" || -d "${ENV_DIR}" || -f "${SYSTEMD_SERVICE}" || "${MIGRATE_FROM_SUI}" == "1" ]]; then
                should_backup=1
            fi
            ;;
    esac

    [[ "${should_backup}" == "1" ]] || return 0

    ensure_backup_space

    local stamp target counter
    stamp="$(date -u +%Y%m%dT%H%M%SZ)"
    target="${BACKUP_ROOT}/${stamp}"
    counter=1
    while [[ -e "${target}" ]]; do
        target="${BACKUP_ROOT}/${stamp}-${counter}"
        counter=$((counter + 1))
    done
    BACKUP_PATH="${target}"

    log "creating backup at ${target}"
    if ! run mkdir -p "${target}"; then
        fail "cannot create backup directory: ${target}"
    fi

    if [[ -d "${INSTALL_DIR}" ]]; then
        copy_backup_path "${INSTALL_DIR}" "${target}/app" "${target}"
    fi
    if [[ -d "${ENV_DIR}" ]]; then
        copy_backup_path "${ENV_DIR}" "${target}/etc" "${target}"
    fi
	if [[ -f "${SYSTEMD_SERVICE}" ]]; then
		copy_backup_path "${SYSTEMD_SERVICE}" "${target}/${SERVICE_NAME}.service" "${target}"
	fi
	if [[ -d "${HARDENED_DATA_ROOT}" ]]; then
		copy_backup_path "${HARDENED_DATA_ROOT}" "${target}/hardened-data" "${target}"
	fi
	backup_systemd_assets "${target}" || return
    if [[ "${MIGRATE_FROM_SUI}" == "1" ]]; then
        if [[ -d "${LEGACY_DIR}" ]]; then
            copy_backup_path "${LEGACY_DIR}" "${target}/legacy-app" "${target}"
        fi
        if [[ -d "${LEGACY_ENV_DIR}" ]]; then
            copy_backup_path "${LEGACY_ENV_DIR}" "${target}/legacy-etc" "${target}"
        fi
        if [[ -f "${LEGACY_SERVICE_FILE}" ]]; then
            copy_backup_path "${LEGACY_SERVICE_FILE}" "${target}/${LEGACY_SERVICE_NAME}.service" "${target}"
        fi
        if [[ -d "${LEGACY_DROPIN_DIR}" ]]; then
            copy_backup_path "${LEGACY_DROPIN_DIR}" "${target}/${LEGACY_SERVICE_NAME}.service.d" "${target}"
        fi
    fi

    if [[ "${DRY_RUN}" != "1" ]]; then
        if ! {
            printf 'app=%s\n' "${APP_NAME}"
            printf 'created_at=%s\n' "${stamp}"
            printf 'install_dir=%s\n' "${INSTALL_DIR}"
            printf 'env_dir=%s\n' "${ENV_DIR}"
            printf 'service=%s\n' "${SYSTEMD_SERVICE}"
			printf 'hardened_data_root=%s\n' "${HARDENED_DATA_ROOT}"
			printf 'systemd_profile_root=%s\n' "${SYSTEMD_PROFILE_ROOT}"
			printf 'systemd_unit_root=%s\n' "${SYSTEMD_UNIT_ROOT}"
            append_backup_build_info "${INSTALL_DIR}/BUILD_INFO.txt"
            if [[ "${MIGRATE_FROM_SUI}" == "1" ]]; then
                printf 'legacy_dir=%s\n' "${LEGACY_DIR}"
                printf 'legacy_env_dir=%s\n' "${LEGACY_ENV_DIR}"
                printf 'legacy_service=%s\n' "${LEGACY_SERVICE_FILE}"
            fi
        } > "${target}/manifest.txt"; then
            cleanup_incomplete_backup "${target}"
            fail "backup failed while writing manifest; removed incomplete backup ${target}"
        fi
    fi
}

append_backup_build_info() {
    local info_file="$1"
    local key value
    [[ -f "${info_file}" ]] || return 0

    while IFS='=' read -r key value || [[ -n "${key}" ]]; do
        case "${key}" in
            app|version|commit|platform|go|sing_box)
                printf 'build_%s=%s\n' "${key}" "${value}"
                ;;
        esac
    done < "${info_file}"
}

restore_backup_dir() {
    local src="$1"
    local dest="$2"

    [[ -d "${src}" ]] || return 0
    local parent base restoring previous
    parent="$(dirname "${dest}")"
    base="$(basename "${dest}")"
    restoring="${parent}/.${base}.restoring.$$"
    previous="${parent}/.${base}.previous.$$"

    mkdir -p "${parent}" || return 1
    rm -rf "${restoring}" "${previous}" || return 1

    if ! cp -a "${src}" "${restoring}"; then
        rm -rf "${restoring}"
        warn "rollback restore failed while copying ${src}; existing ${dest} was left unchanged"
        return 1
    fi

    if [[ -e "${dest}" ]]; then
        if ! mv "${dest}" "${previous}"; then
            rm -rf "${restoring}"
            warn "rollback restore failed while preparing ${dest}; existing data was left unchanged"
            return 1
        fi
    fi

    if ! mv "${restoring}" "${dest}"; then
        if [[ -e "${previous}" && ! -e "${dest}" ]]; then
            mv "${previous}" "${dest}" || true
        fi
        rm -rf "${restoring}"
        warn "rollback restore failed while replacing ${dest}; check the backup at ${BACKUP_PATH}"
        return 1
    fi

    rm -rf "${previous}"
}

backup_has_current_install_payload() {
    local backup="$1"
    [[ -d "${backup}/app" || -d "${backup}/etc" || -f "${backup}/${SERVICE_NAME}.service" ]]
}

restore_current_install_backup() {
    local backup="$1"

    backup_has_current_install_payload "${backup}" || return 1

    systemctl stop "${SERVICE_NAME}" >/dev/null 2>&1 || true
    restore_backup_dir "${backup}/app" "${INSTALL_DIR}" || return 1
    restore_backup_dir "${backup}/etc" "${ENV_DIR}" || return 1
	restore_backup_dir "${backup}/hardened-data" "${HARDENED_DATA_ROOT}" || return 1
	restore_systemd_assets "${backup}" || return 1

    if [[ -f "${backup}/${SERVICE_NAME}.service" ]]; then
        mkdir -p "$(dirname "${SYSTEMD_SERVICE}")"
        cp -a "${backup}/${SERVICE_NAME}.service" "${SYSTEMD_SERVICE}" || return 1
    fi

    if [[ -f "${INSTALL_DIR}/${APP_NAME}.sh" ]]; then
        mkdir -p "$(dirname "${CLI_PATH}")"
        ln -sf "${INSTALL_DIR}/${APP_NAME}.sh" "${CLI_PATH}" || return 1
    fi

    systemctl daemon-reload || return 1
    systemctl restart "${SERVICE_NAME}" || return 1
}

restore_systemd_assets() {
	local backup="$1" inventory="${backup}/systemd-assets/inventory.txt" unit state
	[[ -f "${inventory}" ]] || return 0
	state="$(sed -n 's/^profiles=//p' "${inventory}" | head -n 1)"
	if [[ "${state}" == present ]]; then
		restore_backup_dir "${backup}/systemd-assets/profiles" "${SYSTEMD_PROFILE_ROOT}" || return 1
	elif [[ "${state}" == absent ]]; then
		rm -rf "${SYSTEMD_PROFILE_ROOT}" || return 1
	else
		return 1
	fi
	for unit in solovey-privileged-broker.service solovey-privileged-broker.socket solovey-privileged-proof.socket; do
		state="$(sed -n "s/^${unit}=//p" "${inventory}" | head -n 1)"
		if [[ "${state}" == present && -f "${backup}/systemd-assets/units/${unit}" ]]; then
			mkdir -p "${SYSTEMD_UNIT_ROOT}" || return 1
			cp -a "${backup}/systemd-assets/units/${unit}" "${SYSTEMD_UNIT_ROOT}/${unit}" || return 1
		elif [[ "${state}" == absent ]]; then
			rm -f "${SYSTEMD_UNIT_ROOT}/${unit}" || return 1
		else
			return 1
		fi
	done
}

rollback_failed_install() {
    local status="$1"

    if [[ -z "${BACKUP_PATH}" || ! -f "${BACKUP_PATH}/manifest.txt" ]]; then
        warn "install failed; no previous Solovey UI backup is available for automatic rollback"
        return "${status}"
    fi
    if ! backup_has_current_install_payload "${BACKUP_PATH}"; then
        warn "install failed; backup has no previous Solovey UI payload to restore: ${BACKUP_PATH}"
        return "${status}"
    fi

    warn "install failed; rolling back from ${BACKUP_PATH}"
    if restore_current_install_backup "${BACKUP_PATH}"; then
        warn "rollback after failed install completed"
    else
        warn "rollback after failed install failed; inspect backup: ${BACKUP_PATH}"
    fi
    return "${status}"
}

env_file_has_key() {
    local file="$1"
    local key="$2"
    [[ -f "${file}" ]] && grep -qE "^${key}=" "${file}"
}

append_legacy_env_key_if_missing() {
    local key="$1"
    local line

    env_file_has_key "${SECRETBOX_ENV_FILE}" "${key}" && return 0
    line="$(grep -m1 -E "^${key}=" "${LEGACY_SECRETBOX_ENV_FILE}" 2>/dev/null || true)"
    [[ -n "${line}" ]] || return 0

    if [[ "${DRY_RUN}" == "1" ]]; then
        log "would copy ${key} from ${LEGACY_SECRETBOX_ENV_FILE} to ${SECRETBOX_ENV_FILE}"
        return 0
    fi

    printf '\n%s\n' "${line}" >> "${SECRETBOX_ENV_FILE}"
}

copy_legacy_secretbox_env() {
    [[ "${MIGRATE_FROM_SUI}" == "1" ]] || return 0
    [[ -f "${LEGACY_SECRETBOX_ENV_FILE}" ]] || return 0

    run mkdir -p "${ENV_DIR}"
    if [[ ! -f "${SECRETBOX_ENV_FILE}" ]]; then
        log "copying legacy secretbox env to ${SECRETBOX_ENV_FILE}"
        run cp -a "${LEGACY_SECRETBOX_ENV_FILE}" "${SECRETBOX_ENV_FILE}"
        run chmod 600 "${SECRETBOX_ENV_FILE}"
        return 0
    fi

    append_legacy_env_key_if_missing "SUI_SECRETBOX_KEY"
    append_legacy_env_key_if_missing "SUI_COOKIE_KEY"
    append_legacy_env_key_if_missing "SUI_SECRET"
    run chmod 600 "${SECRETBOX_ENV_FILE}"
}

create_secretbox_env() {
    local key secret

    if [[ ! -f "${SECRETBOX_ENV_FILE}" ]]; then
        log "creating ${SECRETBOX_ENV_FILE}"
    fi
    run mkdir -p "${ENV_DIR}"

    if [[ "${DRY_RUN}" == "1" ]]; then
        log "would ensure SUI_SECRETBOX_KEY and SUI_COOKIE_KEY in ${SECRETBOX_ENV_FILE}"
        return 0
    fi

    umask 077
    touch "${SECRETBOX_ENV_FILE}"
    for key in SUI_SECRETBOX_KEY SUI_COOKIE_KEY; do
        if grep -Eq "^${key}=" "${SECRETBOX_ENV_FILE}" 2>/dev/null; then
            continue
        fi
        secret="$(dd if=/dev/urandom bs=32 count=1 2>/dev/null | base64 | tr -d '\n')"
        [[ -n "${secret}" ]] || fail "failed to generate ${key}"
        printf '%s=%s\n' "${key}" "${secret}" >> "${SECRETBOX_ENV_FILE}"
    done
    chmod 600 "${SECRETBOX_ENV_FILE}"
}

atomic_install_file() {
    local source="$1"
    local destination="$2"
    local mode="$3"
    local incoming="${destination}.incoming.$$"

    mkdir -p "$(dirname "${destination}")"
    rm -f "${incoming}"
    cp -a "${source}" "${incoming}" || { rm -f "${incoming}"; return 1; }
    chmod "${mode}" "${incoming}" || { rm -f "${incoming}"; return 1; }
	chown 0:0 "${incoming}" || { rm -f "${incoming}"; return 1; }
    mv -f "${incoming}" "${destination}" || { rm -f "${incoming}"; return 1; }
}

stop_existing_service() {
    if systemctl list-unit-files "${SERVICE_NAME}.service" >/dev/null 2>&1 || [[ -f "${SYSTEMD_SERVICE}" ]]; then
        if systemctl is-active --quiet "${SERVICE_NAME}" >/dev/null 2>&1; then
            log "stopping ${SERVICE_NAME}"
            run systemctl stop "${SERVICE_NAME}"
        fi
    fi
}

stop_legacy_service_for_migration() {
    [[ "${MIGRATE_FROM_SUI}" == "1" ]] || return 0

    if systemctl list-unit-files "${LEGACY_SERVICE_NAME}.service" >/dev/null 2>&1 || [[ -f "${LEGACY_SERVICE_FILE}" ]]; then
        if systemctl is-active --quiet "${LEGACY_SERVICE_NAME}" >/dev/null 2>&1; then
            log "stopping legacy ${LEGACY_SERVICE_NAME}"
            run systemctl stop "${LEGACY_SERVICE_NAME}"
        fi
        log "disabling legacy ${LEGACY_SERVICE_NAME}"
        run systemctl disable "${LEGACY_SERVICE_NAME}" >/dev/null 2>&1 || true
    fi

    if [[ -x "${LEGACY_DIR}/bin/sing-box" ]] && systemctl is-active --quiet sing-box >/dev/null 2>&1; then
        log "stopping legacy sing-box service managed from ${LEGACY_DIR}/bin"
        run systemctl stop sing-box
    fi
}

copy_legacy_db_sidecar() {
    local suffix="$1"
    local source="${LEGACY_DB}${suffix}"
    local target="${TARGET_DB}${suffix}"

    if [[ -f "${source}" ]]; then
        run cp -a "${source}" "${target}"
    fi
}

rewrite_legacy_paths_in_db() {
    [[ "${DRY_RUN}" != "1" ]] || return 0

    sqlite3 "${TARGET_DB}" <<SQL
UPDATE settings
   SET value = replace(value, '/usr/local/s-ui/', '/usr/local/solovey-ui/')
 WHERE value LIKE '%/usr/local/s-ui/%';
SQL
}

migrate_legacy_data() {
    [[ "${MIGRATE_FROM_SUI}" == "1" ]] || return 0

    log "migrating legacy s-ui data"
    run mkdir -p "${INSTALL_DIR}/db"

    if [[ -f "${TARGET_DB}" && "${FORCE_MIGRATE}" == "1" ]]; then
        warn "replacing existing target DB because --force-migrate was provided: ${TARGET_DB}"
        run rm -f "${TARGET_DB}" "${TARGET_DB}-wal" "${TARGET_DB}-shm"
    fi

    run cp -a "${LEGACY_DB}" "${TARGET_DB}"
    copy_legacy_db_sidecar "-wal"
    copy_legacy_db_sidecar "-shm"

    if [[ -d "${LEGACY_CERT_DIR}" ]]; then
        if [[ -e "${INSTALL_DIR}/cert" ]]; then
            warn "target cert directory already exists, not overwriting: ${INSTALL_DIR}/cert"
        else
            run cp -a "${LEGACY_CERT_DIR}" "${INSTALL_DIR}/cert"
        fi
    fi

    rewrite_legacy_paths_in_db
}

install_payload() {
    local payload_dir="$1"
	local release_digest release_id release_root release_staging current_incoming name

    [[ -d "${payload_dir}" ]] || fail "release payload directory not found: ${payload_dir}"
    [[ -f "${payload_dir}/${APP_NAME}" ]] || fail "release payload misses ${APP_NAME} binary"
    [[ -f "${payload_dir}/${APP_NAME}.sh" ]] || fail "release payload misses ${APP_NAME}.sh"
    [[ -f "${payload_dir}/${SERVICE_NAME}.service" ]] || fail "release payload misses ${SERVICE_NAME}.service"
	[[ -x "${payload_dir}/solovey-privileged-broker" ]] || fail "release payload misses privileged broker"
	[[ -x "${payload_dir}/solovey-ssh-proof" ]] || fail "release payload misses SSH proof client"
	[[ -x "${payload_dir}/solovey-broker-manifest" ]] || fail "release payload misses broker manifest writer"
	[[ -d "${payload_dir}/systemd" ]] || fail "release payload misses systemd profiles"
    [[ -f "${payload_dir}/BUILD_INFO.txt" ]] || fail "release payload misses BUILD_INFO.txt"

	detect_deployment_profile
	verify_systemd_profile_support || return
    stop_existing_service || return
    stop_legacy_service_for_migration || return

	release_digest="$(
		cd "${payload_dir}"
		find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum | sha256sum | awk '{print substr($1,1,16)}'
	)"
	[[ "${release_digest}" =~ ^[a-f0-9]{16}$ ]] || fail "release-set digest is invalid"
	release_id="installer-${release_digest}"
	release_root="${RELEASES_DIR}/${release_id}"
	release_staging="${release_root}.incoming.$$"
	current_incoming="${RELEASES_DIR}/.current-${release_id}.$$"
	run mkdir -p "${INSTALL_DIR}" "${INSTALL_DIR}/db" "${RELEASES_DIR}" "${ENV_DIR}" "${SYSTEMD_SERVICE%/*}" "${CLI_PATH%/*}" "${SYSTEMD_PROFILE_ROOT}" || return
	if [[ ! -d "${release_root}" ]]; then
		run rm -rf "${release_staging}" || return
		run mkdir -p "${release_staging}" || return
		run cp -a "${payload_dir}/." "${release_staging}/" || return
		run chown -R 0:0 "${release_staging}" || return
		run chmod 755 "${release_staging}/${APP_NAME}" "${release_staging}/${APP_NAME}.sh" \
			"${release_staging}/solovey-privileged-broker" "${release_staging}/solovey-ssh-proof" \
			"${release_staging}/solovey-broker-manifest" || return
		if [[ -f "${release_staging}/solovey-protect-helper" ]]; then
			run chmod 755 "${release_staging}/solovey-protect-helper" || return
		fi
		run chmod 644 "${release_staging}/${SERVICE_NAME}.service" "${release_staging}/BUILD_INFO.txt" || return
		run mv "${release_staging}" "${release_root}" || return
	fi
	run rm -f "${current_incoming}" || return
	run ln -s "${release_id}" "${current_incoming}" || return
	run mv -Tf "${current_incoming}" "${CURRENT_RELEASE_DIR}" || return
	for name in "${APP_NAME}" "${APP_NAME}.sh" solovey-privileged-broker solovey-ssh-proof solovey-broker-manifest "${SERVICE_NAME}.service" BUILD_INFO.txt; do
		run ln -sfn "releases/current/${name}" "${INSTALL_DIR}/${name}" || return
	done
	if [[ -f "${CURRENT_RELEASE_DIR}/solovey-protect-helper" ]]; then
		run ln -sfn "releases/current/solovey-protect-helper" "${INSTALL_DIR}/solovey-protect-helper" || return
	else
		run rm -f "${INSTALL_DIR}/solovey-protect-helper" || return
	fi
	install_systemd_profiles "${payload_dir}/systemd" || return
	run chown root:solovey-ui "${CURRENT_RELEASE_DIR}/solovey-ssh-proof" || return
	run chmod 2755 "${CURRENT_RELEASE_DIR}/solovey-ssh-proof" || return
    run ln -sf "${MANAGER_PATH}" "${CLI_PATH}" || return
    copy_legacy_secretbox_env || return
    create_secretbox_env || return
    migrate_legacy_data || return
    install_component_packs || return
    write_component_metadata || return

	configure_deployment_profile || return
	run systemctl daemon-reload || return
	run systemctl enable solovey-privileged-broker.socket solovey-privileged-proof.socket || return
	run "${CURRENT_RELEASE_DIR}/solovey-broker-manifest" || return
	if [[ "${DEPLOYMENT_PROFILE}" == "native-legacy-root" ]]; then
		run env SUI_DB_FOLDER="${INSTALL_DIR}/db" "${BIN_PATH}" migrate || return
	else
		run runuser -u solovey-ui -- env SUI_DB_FOLDER="${HARDENED_DATA_ROOT}/db" "${BIN_PATH}" migrate || return
	fi
    run systemctl enable "${SERVICE_NAME}" || return
    run systemctl restart "${SERVICE_NAME}" || return
}

detect_deployment_profile() {
	local installed=""
	if [[ -f "${DEPLOYMENT_MARKER}" ]]; then
		installed="$(tr -d '\r\n' < "${DEPLOYMENT_MARKER}")"
	fi
	case "${installed}" in
		native-hardened|native-network-advanced|native-legacy-root) DEPLOYMENT_PROFILE="${installed}" ;;
		*)
			if [[ "${MIGRATE_FROM_SUI}" == "1" || -f "${SYSTEMD_SERVICE}" || -f "${INSTALL_DIR}/db/${APP_NAME}.db" ]]; then
				DEPLOYMENT_PROFILE="native-legacy-root"
			else
				DEPLOYMENT_PROFILE="native-hardened"
			fi
			;;
	esac
	if [[ "${DEPLOYMENT_PROFILE}" == "native-legacy-root" ]]; then
		TARGET_DB="${INSTALL_DIR}/db/${APP_NAME}.db"
	else
		TARGET_DB="${HARDENED_DATA_ROOT}/db/${APP_NAME}.db"
	fi
	log "deployment profile: ${DEPLOYMENT_PROFILE}"
}

install_systemd_profiles() {
	local source="$1" unit
	for unit in solovey-ui-native-hardened.service solovey-ui-native-network-advanced.service solovey-ui-native-legacy-root.service \
		solovey-privileged-broker.service solovey-privileged-broker.socket solovey-privileged-proof.socket solovey-ui.sysusers solovey-ui.tmpfiles; do
		[[ -f "${source}/${unit}" ]] || fail "release payload misses systemd asset ${unit}"
		atomic_install_file "${source}/${unit}" "${SYSTEMD_PROFILE_ROOT}/${unit}" 644 || return
	done
	atomic_install_file "${source}/solovey-privileged-broker.service" "${SYSTEMD_UNIT_ROOT}/solovey-privileged-broker.service" 644 || return
	atomic_install_file "${source}/solovey-privileged-broker.socket" "${SYSTEMD_UNIT_ROOT}/solovey-privileged-broker.socket" 644 || return
	atomic_install_file "${source}/solovey-privileged-proof.socket" "${SYSTEMD_UNIT_ROOT}/solovey-privileged-proof.socket" 644 || return
	run systemd-sysusers "${source}/solovey-ui.sysusers" || return
	run systemd-tmpfiles --create "${source}/solovey-ui.tmpfiles" || return
}

configure_deployment_profile() {
	local target="${SYSTEMD_PROFILE_ROOT}/solovey-ui-${DEPLOYMENT_PROFILE}.service" incoming="${SYSTEMD_SERVICE}.incoming.$$"
	[[ -f "${target}" ]] || fail "selected deployment profile is not packaged: ${DEPLOYMENT_PROFILE}"
	if [[ "${DRY_RUN}" == "1" ]]; then
		log "would select ${target} as ${SYSTEMD_SERVICE}"
		return 0
	fi
	rm -f "${incoming}"
	ln -s "${target}" "${incoming}" || return
	mv -Tf "${incoming}" "${SYSTEMD_SERVICE}" || return
	printf '%s\n' "${DEPLOYMENT_PROFILE}" > "${DEPLOYMENT_MARKER}.incoming"
	chmod 0644 "${DEPLOYMENT_MARKER}.incoming"
	mv -f "${DEPLOYMENT_MARKER}.incoming" "${DEPLOYMENT_MARKER}"
}

verify_systemd_profile_support() {
	[[ "${DRY_RUN}" == "1" || "${DEPLOYMENT_PROFILE}" == "native-legacy-root" ]] && return 0
	if [[ "${DEPLOYMENT_PROFILE}" == "native-network-advanced" ]]; then
		fail "native-network-advanced is generated but unavailable until a separately confined core runtime exists"
	fi
	local first version
	first="$(LC_ALL=C systemctl --version 2>/dev/null | sed -n '1p')" || fail "unable to inspect installed systemd capabilities"
	if [[ "${first}" =~ ^systemd[[:space:]]+([0-9]+) ]]; then
		version="${BASH_REMATCH[1]}"
	else
		fail "unable to parse installed systemd capability version"
	fi
	(( version >= 249 )) || fail "native-hardened requires systemd 249 or newer; no legacy-root fallback is selected for a fresh install"
}

download_and_install() {
    local platform artifact version url checksum_url components_artifact components_url components_checksum_url tmp_dir payload_dir install_status
    platform="$(detect_arch)"
    version="${VERSION:-$(latest_version)}"
    artifact="$(release_artifact_name "${platform}")"
    url="${GITHUB_RELEASES}/${version}/${artifact}"
    checksum_url="${url}.sha256"
    components_artifact="$(components_bundle_artifact_name)"
    components_url="${GITHUB_RELEASES}/${version}/${components_artifact}"
    components_checksum_url="${components_url}.sha256"

    log "release: ${version}"
    log "platform: linux/${platform}"
    log "requested profile: ${REQUESTED_PROFILE}"
    log "resolved profile: $(component_profile_name)"
    log "binary: ${BINARY_PROFILE}"
    log "components: $(component_summary)"
    log "artifact: ${artifact}"
    log "install dir: ${INSTALL_DIR}"
    log "service: ${SERVICE_NAME}"

    maybe_warn_legacy_install
    describe_legacy_migration_plan

    if [[ "${DRY_RUN}" == "1" ]]; then
        backup_existing
        log "would download ${url}"
        log "would verify ${checksum_url}"
        if any_component_installed; then
            log "would download ${components_url}"
            log "would verify ${components_checksum_url}"
        else
            log "would skip component bundle download because no optional components are selected"
        fi
        log "would install ${APP_NAME} and restart ${SERVICE_NAME}"
        log "would install component profile $(component_profile_name) with ${BINARY_PROFILE} binary: $(component_summary)"
        if [[ "${MIGRATE_FROM_SUI}" == "1" ]]; then
            log "would stop and disable legacy ${LEGACY_SERVICE_NAME}, copy DB/env/cert, rewrite legacy paths, then run ${APP_NAME} migrate"
        fi
        return 0
    fi

    require_root
    validate_legacy_migration_ready
    backup_existing

    tmp_dir="$(mktemp -d)"
    DOWNLOAD_TMP_DIR="${tmp_dir}"
    trap 'if [[ -n "${DOWNLOAD_TMP_DIR:-}" ]]; then rm -rf "${DOWNLOAD_TMP_DIR}"; fi' EXIT

    log "downloading ${url}"
    secure_curl -o "${tmp_dir}/${artifact}" "${url}"
    secure_curl -o "${tmp_dir}/${artifact}.sha256" "${checksum_url}"
    if any_component_installed; then
        log "downloading ${components_url}"
        secure_curl -o "${tmp_dir}/${components_artifact}" "${components_url}"
        secure_curl -o "${tmp_dir}/${components_artifact}.sha256" "${components_checksum_url}"
    fi

    log "verifying checksum"
    (
        cd "${tmp_dir}"
        sha256sum -c "${artifact}.sha256"
        if any_component_installed; then
            sha256sum -c "${components_artifact}.sha256"
        fi
    )

    log "extracting release"
    safe_extract_tar "${tmp_dir}/${artifact}" "${tmp_dir}"
    payload_dir="${tmp_dir}/${APP_NAME}"
    if any_component_installed; then
        log "extracting component bundle"
        safe_extract_tar "${tmp_dir}/${components_artifact}" "${tmp_dir}"
        COMPONENT_PAYLOAD_DIR="${tmp_dir}/components"
        load_component_ids_from_payload "${COMPONENT_PAYLOAD_DIR}"
        validate_component_payload
    fi

    install_status=0
    install_payload "${payload_dir}" || install_status=$?
    if [[ "${install_status}" != "0" ]]; then
        rollback_failed_install "${install_status}"
    fi

    log "${APP_NAME} ${version} is installed and running"
    if [[ -n "${BACKUP_PATH}" ]]; then
        log "backup: ${BACKUP_PATH}"
    fi
    if [[ -f "${INSTALL_DIR}/db/initial-admin.txt" ]]; then
        log "initial admin credentials: ${INSTALL_DIR}/db/initial-admin.txt"
    else
        log "use '${APP_NAME} admin -show' to inspect the current admin account"
    fi
}

if [[ -n "${COMPONENT_IDS_RAW}" ]]; then
    append_component_list "with" "${COMPONENT_IDS_RAW}"
fi
parse_args "$@"
require_tools
download_and_install
