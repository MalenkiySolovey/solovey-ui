#!/usr/bin/env bash

set -Eeuo pipefail

APP_NAME="solovey-ui"
SERVICE_NAME="solovey-ui"
REPO="${SOLOVEY_UI_REPO:-MalenkiySolovey/solovey-ui}"

INSTALL_DIR="${SOLOVEY_UI_INSTALL_DIR:-/usr/local/${APP_NAME}}"
BIN_PATH="${INSTALL_DIR}/${APP_NAME}"
MANAGER_PATH="${INSTALL_DIR}/${APP_NAME}.sh"
CLI_PATH="${SOLOVEY_UI_CLI_PATH:-/usr/bin/${APP_NAME}}"
SYSTEMD_SERVICE="${SOLOVEY_UI_SYSTEMD_SERVICE:-/etc/systemd/system/${SERVICE_NAME}.service}"
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

DRY_RUN=0
NON_INTERACTIVE=0
BACKUP_MODE="auto"
MIGRATE_FROM_SUI=0
FORCE_MIGRATE=0
VERSION=""
BACKUP_PATH=""
DOWNLOAD_TMP_DIR=""

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
  --migrate-from-sui     Copy a legacy /usr/local/s-ui install into Solovey UI.
  --force-migrate        Allow --migrate-from-sui to replace an existing new DB.
  --help, -h             Show this help.

Examples:
  bash install.sh
  bash install.sh --version v1.5.7-solovey.1
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

require_tools() {
    require_command uname
    require_command curl
    require_command sed
    require_command grep

    if [[ "${DRY_RUN}" != "1" ]]; then
        require_command tar
        require_command sha256sum
        require_command systemctl
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
        curl -fsSL \
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
    run mkdir -p "${target}"

    if [[ -d "${INSTALL_DIR}" ]]; then
        run cp -a "${INSTALL_DIR}" "${target}/app"
    fi
    if [[ -d "${ENV_DIR}" ]]; then
        run cp -a "${ENV_DIR}" "${target}/etc"
    fi
    if [[ -f "${SYSTEMD_SERVICE}" ]]; then
        run cp -a "${SYSTEMD_SERVICE}" "${target}/${SERVICE_NAME}.service"
    fi
    if [[ "${MIGRATE_FROM_SUI}" == "1" ]]; then
        if [[ -d "${LEGACY_DIR}" ]]; then
            run cp -a "${LEGACY_DIR}" "${target}/legacy-app"
        fi
        if [[ -d "${LEGACY_ENV_DIR}" ]]; then
            run cp -a "${LEGACY_ENV_DIR}" "${target}/legacy-etc"
        fi
        if [[ -f "${LEGACY_SERVICE_FILE}" ]]; then
            run cp -a "${LEGACY_SERVICE_FILE}" "${target}/${LEGACY_SERVICE_NAME}.service"
        fi
        if [[ -d "${LEGACY_DROPIN_DIR}" ]]; then
            run cp -a "${LEGACY_DROPIN_DIR}" "${target}/${LEGACY_SERVICE_NAME}.service.d"
        fi
    fi

    if [[ "${DRY_RUN}" != "1" ]]; then
        {
            printf 'app=%s\n' "${APP_NAME}"
            printf 'created_at=%s\n' "${stamp}"
            printf 'install_dir=%s\n' "${INSTALL_DIR}"
            printf 'env_dir=%s\n' "${ENV_DIR}"
            printf 'service=%s\n' "${SYSTEMD_SERVICE}"
            append_backup_build_info "${INSTALL_DIR}/BUILD_INFO.txt"
            if [[ "${MIGRATE_FROM_SUI}" == "1" ]]; then
                printf 'legacy_dir=%s\n' "${LEGACY_DIR}"
                printf 'legacy_env_dir=%s\n' "${LEGACY_ENV_DIR}"
                printf 'legacy_service=%s\n' "${LEGACY_SERVICE_FILE}"
            fi
        } > "${target}/manifest.txt"
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
    rm -rf "${dest}"
    mkdir -p "$(dirname "${dest}")"
    cp -a "${src}" "${dest}"
}

backup_has_current_install_payload() {
    local backup="$1"
    [[ -d "${backup}/app" || -d "${backup}/etc" || -f "${backup}/${SERVICE_NAME}.service" ]]
}

restore_current_install_backup() {
    local backup="$1"

    backup_has_current_install_payload "${backup}" || return 1

    systemctl stop "${SERVICE_NAME}" >/dev/null 2>&1 || true
    restore_backup_dir "${backup}/app" "${INSTALL_DIR}"
    restore_backup_dir "${backup}/etc" "${ENV_DIR}"

    if [[ -f "${backup}/${SERVICE_NAME}.service" ]]; then
        mkdir -p "$(dirname "${SYSTEMD_SERVICE}")"
        cp -a "${backup}/${SERVICE_NAME}.service" "${SYSTEMD_SERVICE}"
    fi

    if [[ -f "${INSTALL_DIR}/${APP_NAME}.sh" ]]; then
        mkdir -p "$(dirname "${CLI_PATH}")"
        ln -sf "${INSTALL_DIR}/${APP_NAME}.sh" "${CLI_PATH}"
    fi

    systemctl daemon-reload
    systemctl restart "${SERVICE_NAME}"
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
    if [[ -f "${SECRETBOX_ENV_FILE}" ]]; then
        run chmod 600 "${SECRETBOX_ENV_FILE}"
        return 0
    fi

    log "creating ${SECRETBOX_ENV_FILE}"
    run mkdir -p "${ENV_DIR}"

    if [[ "${DRY_RUN}" == "1" ]]; then
        log "would create SUI_SECRETBOX_KEY in ${SECRETBOX_ENV_FILE}"
        return 0
    fi

    local secret
    secret="$(dd if=/dev/urandom bs=32 count=1 2>/dev/null | base64 | tr -d '\n')"
    umask 077
    printf 'SUI_SECRETBOX_KEY=%s\n' "${secret}" > "${SECRETBOX_ENV_FILE}"
    chmod 600 "${SECRETBOX_ENV_FILE}"
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

    [[ -d "${payload_dir}" ]] || fail "release payload directory not found: ${payload_dir}"
    [[ -f "${payload_dir}/${APP_NAME}" ]] || fail "release payload misses ${APP_NAME} binary"
    [[ -f "${payload_dir}/${APP_NAME}.sh" ]] || fail "release payload misses ${APP_NAME}.sh"
    [[ -f "${payload_dir}/${SERVICE_NAME}.service" ]] || fail "release payload misses ${SERVICE_NAME}.service"

    stop_existing_service || return
    stop_legacy_service_for_migration || return

    run mkdir -p "${INSTALL_DIR}" "${INSTALL_DIR}/db" "${ENV_DIR}" "${SYSTEMD_SERVICE%/*}" "${CLI_PATH%/*}" || return
    run cp -a "${payload_dir}/." "${INSTALL_DIR}/" || return
    run chmod 755 "${BIN_PATH}" "${MANAGER_PATH}" || return
    run cp -f "${payload_dir}/${SERVICE_NAME}.service" "${SYSTEMD_SERVICE}" || return
    run ln -sf "${MANAGER_PATH}" "${CLI_PATH}" || return
    copy_legacy_secretbox_env || return
    create_secretbox_env || return
    migrate_legacy_data || return

    run systemctl daemon-reload || return
    run "${BIN_PATH}" migrate || return
    run systemctl enable "${SERVICE_NAME}" || return
    run systemctl restart "${SERVICE_NAME}" || return
}

download_and_install() {
    local platform artifact version url checksum_url tmp_dir payload_dir install_status
    platform="$(detect_arch)"
    version="${VERSION:-$(latest_version)}"
    artifact="${APP_NAME}-linux-${platform}.tar.gz"
    url="${GITHUB_RELEASES}/${version}/${artifact}"
    checksum_url="${url}.sha256"

    log "release: ${version}"
    log "platform: linux/${platform}"
    log "artifact: ${artifact}"
    log "install dir: ${INSTALL_DIR}"
    log "service: ${SERVICE_NAME}"

    maybe_warn_legacy_install
    describe_legacy_migration_plan

    if [[ "${DRY_RUN}" == "1" ]]; then
        backup_existing
        log "would download ${url}"
        log "would verify ${checksum_url}"
        log "would install ${APP_NAME} and restart ${SERVICE_NAME}"
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
    curl -fL --proto '=https' --tlsv1.2 -o "${tmp_dir}/${artifact}" "${url}"
    curl -fL --proto '=https' --tlsv1.2 -o "${tmp_dir}/${artifact}.sha256" "${checksum_url}"

    log "verifying checksum"
    (
        cd "${tmp_dir}"
        sha256sum -c "${artifact}.sha256"
    )

    log "extracting release"
    tar -xzf "${tmp_dir}/${artifact}" -C "${tmp_dir}"
    payload_dir="${tmp_dir}/${APP_NAME}"

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

parse_args "$@"
require_tools
download_and_install
