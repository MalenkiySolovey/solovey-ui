#!/usr/bin/env bash

set -Eeuo pipefail

APP_NAME="solovey-ui"
SERVICE_NAME="solovey-ui"
INSTALL_DIR="${SOLOVEY_UI_INSTALL_DIR:-/usr/local/${APP_NAME}}"
BIN_PATH="${INSTALL_DIR}/${APP_NAME}"
CLI_PATH="${SOLOVEY_UI_CLI_PATH:-/usr/bin/${APP_NAME}}"
SERVICE_FILE="${SOLOVEY_UI_SYSTEMD_SERVICE:-/etc/systemd/system/${SERVICE_NAME}.service}"
ENV_DIR="${SOLOVEY_UI_ENV_DIR:-/etc/${APP_NAME}}"
BACKUP_ROOT="${SOLOVEY_UI_BACKUP_ROOT:-/var/backups/${APP_NAME}}"
INSTALL_URL="${SOLOVEY_UI_INSTALL_URL:-https://raw.githubusercontent.com/MalenkiySolovey/solovey-ui/main/install.sh}"

usage() {
    cat <<EOF
Solovey UI management script

Usage:
  solovey-ui <command> [args]

Service commands:
  start                Start ${SERVICE_NAME}
  stop                 Stop ${SERVICE_NAME}
  restart              Restart ${SERVICE_NAME}
  status               Show service status
  enable               Enable autostart
  disable              Disable autostart
  log                  Follow service logs

Panel commands:
  uri                  Show panel URI
  admin [args]         Run admin CLI command
  setting [args]       Run setting CLI command
  migrate [args]       Run database migrations
  import-xui [args]    Run 3x-ui/x-ui import command
  decrypt-backup [args] Run backup decrypt command
  version, -v          Show binary version
  build-info           Show release build metadata

Maintenance:
  install [args]       Download and run the installer
  update [args]        Download and run the installer
  migrate-from-sui [args] Download installer and migrate /usr/local/s-ui
  doctor               Run post-install/update smoke checks
  backup               Create a local backup
  rollback [backup]    Restore a backup directory (default: latest)
  uninstall [--purge]  Remove service and command; --purge also removes data
  help                 Show this help
EOF
}

log() {
    printf '[%s] %s\n' "${APP_NAME}" "$*"
}

fail() {
    printf '[%s] ERROR: %s\n' "${APP_NAME}" "$*" >&2
    exit 1
}

need_root() {
    if [[ "${SOLOVEY_UI_ALLOW_NON_ROOT:-0}" == "1" ]]; then
        return 0
    fi
    [[ "${EUID}" -eq 0 ]] || fail "run as root, for example: sudo ${APP_NAME} $*"
}

need_binary() {
    [[ -x "${BIN_PATH}" ]] || fail "binary not found or not executable: ${BIN_PATH}"
}

systemctl_cmd() {
    need_root "$1"
    systemctl "$1" "${SERVICE_NAME}"
}

show_status() {
    systemctl status "${SERVICE_NAME}" -l --no-pager
}

show_log() {
    journalctl -u "${SERVICE_NAME}" -e --no-pager -f
}

run_binary() {
    need_binary
    "${BIN_PATH}" "$@"
}

print_build_info() {
    local info_file="${INSTALL_DIR}/BUILD_INFO.txt"
    [[ -f "${info_file}" ]] || fail "build metadata not found: ${info_file}"

    while IFS= read -r line || [[ -n "${line}" ]]; do
        printf '%s\n' "${line}"
    done < "${info_file}"
}

show_version() {
    run_binary -v
    if [[ -f "${INSTALL_DIR}/BUILD_INFO.txt" ]]; then
        printf '\nBuild metadata:\n'
        print_build_info
    fi
}

doctor_ok() {
    printf '[OK] %s\n' "$*"
}

doctor_fail() {
    printf '[FAIL] %s\n' "$*" >&2
    doctor_failures=$((${doctor_failures:-0} + 1))
}

doctor_require_file() {
    local label="$1"
    local path="$2"

    if [[ -f "${path}" ]]; then
        doctor_ok "${label}: ${path}"
    else
        doctor_fail "${label} missing: ${path}"
    fi
}

doctor_require_executable() {
    local label="$1"
    local path="$2"

    if [[ -x "${path}" ]]; then
        doctor_ok "${label}: ${path}"
    else
        doctor_fail "${label} missing or not executable: ${path}"
    fi
}

doctor_build_value() {
    local key="$1"
    local file="$2"

    sed -nE "s/^${key}=(.*)$/\1/p" "${file}" 2>/dev/null | head -n 1
}

run_doctor() {
    local doctor_failures=0
    local build_info="${INSTALL_DIR}/BUILD_INFO.txt"
    local version=""
    local sing_box=""

    doctor_require_executable "binary" "${BIN_PATH}"
    doctor_require_executable "manager script" "${INSTALL_DIR}/${APP_NAME}.sh"
    doctor_require_executable "CLI command" "${CLI_PATH}"
    doctor_require_file "systemd service file" "${SERVICE_FILE}"
    doctor_require_file "database" "${INSTALL_DIR}/db/${APP_NAME}.db"
    doctor_require_file "secret env" "${ENV_DIR}/secretbox.env"

    if [[ -f "${ENV_DIR}/secretbox.env" ]] && grep -Eq '^SUI_SECRETBOX_KEY=' "${ENV_DIR}/secretbox.env" 2>/dev/null; then
        doctor_ok "secret env contains SUI_SECRETBOX_KEY"
    else
        doctor_fail "secret env missing SUI_SECRETBOX_KEY: ${ENV_DIR}/secretbox.env"
    fi

    doctor_require_file "build metadata" "${build_info}"
    if [[ -f "${build_info}" ]]; then
        version="$(doctor_build_value version "${build_info}")"
        sing_box="$(doctor_build_value sing_box "${build_info}")"

        if [[ -n "${version}" ]]; then
            doctor_ok "build version=${version}"
        else
            doctor_fail "build metadata missing version: ${build_info}"
        fi

        if [[ -n "${sing_box}" ]]; then
            doctor_ok "embedded sing-box=${sing_box}"
        else
            doctor_fail "build metadata missing sing_box: ${build_info}"
        fi
    fi

    if command -v systemctl >/dev/null 2>&1; then
        if systemctl is-active --quiet "${SERVICE_NAME}"; then
            doctor_ok "service active: ${SERVICE_NAME}"
        else
            doctor_fail "service is not active: ${SERVICE_NAME}"
        fi
    else
        doctor_fail "systemctl not found"
    fi

    if [[ "${doctor_failures}" -gt 0 ]]; then
        fail "doctor found ${doctor_failures} failure(s)"
    fi

    log "doctor checks passed"
}

run_installer() {
    need_root "${1:-install}"
    command -v curl >/dev/null 2>&1 || fail "curl is required"

    local tmp
    tmp="$(mktemp)"
    trap 'rm -f "${tmp}"' RETURN

    curl -fsSL --proto '=https' --tlsv1.2 -o "${tmp}" "${INSTALL_URL}"
    bash "${tmp}" "$@"
}

backup_local() {
    need_root backup

    local stamp target counter
    stamp="$(date -u +%Y%m%dT%H%M%SZ)"
    target="${BACKUP_ROOT}/${stamp}"
    counter=1
    while [[ -e "${target}" ]]; do
        target="${BACKUP_ROOT}/${stamp}-${counter}"
        counter=$((counter + 1))
    done

    mkdir -p "${target}"
    if [[ -d "${INSTALL_DIR}" ]]; then
        cp -a "${INSTALL_DIR}" "${target}/app"
    fi
    if [[ -d "${ENV_DIR}" ]]; then
        cp -a "${ENV_DIR}" "${target}/etc"
    fi
    if [[ -f "${SERVICE_FILE}" ]]; then
        cp -a "${SERVICE_FILE}" "${target}/${SERVICE_NAME}.service"
    fi

    {
        printf 'app=%s\n' "${APP_NAME}"
        printf 'created_at=%s\n' "${stamp}"
        printf 'install_dir=%s\n' "${INSTALL_DIR}"
        printf 'env_dir=%s\n' "${ENV_DIR}"
        printf 'service=%s\n' "${SERVICE_FILE}"
        append_backup_build_info "${INSTALL_DIR}/BUILD_INFO.txt"
    } > "${target}/manifest.txt"

    log "backup created at ${target}"
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

latest_backup() {
    [[ -d "${BACKUP_ROOT}" ]] || return 1
    find "${BACKUP_ROOT}" -mindepth 1 -maxdepth 1 -type d | sort | tail -n 1
}

resolve_backup() {
    local requested="${1:-latest}"
    local backup=""

    if [[ "${requested}" == "latest" ]]; then
        backup="$(latest_backup || true)"
    elif [[ -d "${requested}" ]]; then
        backup="${requested}"
    elif [[ -d "${BACKUP_ROOT}/${requested}" ]]; then
        backup="${BACKUP_ROOT}/${requested}"
    fi

    [[ -n "${backup}" && -d "${backup}" ]] || fail "backup not found: ${requested}"
    [[ -f "${backup}/manifest.txt" ]] || fail "backup manifest not found: ${backup}/manifest.txt"
    printf '%s\n' "${backup}"
}

restore_backup_dir() {
    local src="$1"
    local dest="$2"

    [[ -d "${src}" ]] || return 0
    rm -rf "${dest}"
    mkdir -p "$(dirname "${dest}")"
    cp -a "${src}" "${dest}"
}

rollback_backup() {
    need_root rollback
    if [[ $# -gt 1 ]]; then
        fail "rollback accepts at most one backup path"
    fi

    local backup
    backup="$(resolve_backup "${1:-latest}")"
    if [[ ! -d "${backup}/app" && ! -d "${backup}/etc" && ! -f "${backup}/${SERVICE_NAME}.service" ]]; then
        fail "backup has no restorable app/etc/service payload: ${backup}"
    fi

    log "rolling back from ${backup}"
    log "creating safety backup before rollback"
    backup_local

    systemctl stop "${SERVICE_NAME}" >/dev/null 2>&1 || true
    restore_backup_dir "${backup}/app" "${INSTALL_DIR}"
    restore_backup_dir "${backup}/etc" "${ENV_DIR}"

    if [[ -f "${backup}/${SERVICE_NAME}.service" ]]; then
        mkdir -p "$(dirname "${SERVICE_FILE}")"
        cp -a "${backup}/${SERVICE_NAME}.service" "${SERVICE_FILE}"
    fi

    if [[ -f "${INSTALL_DIR}/${APP_NAME}.sh" ]]; then
        mkdir -p "$(dirname "${CLI_PATH}")"
        ln -sf "${INSTALL_DIR}/${APP_NAME}.sh" "${CLI_PATH}"
    fi

    systemctl daemon-reload
    systemctl restart "${SERVICE_NAME}"
    log "rollback complete"
}

uninstall() {
    need_root uninstall

    local purge=0
    if [[ "${1:-}" == "--purge" ]]; then
        purge=1
    elif [[ $# -gt 0 ]]; then
        fail "unknown uninstall option: $1"
    fi

    backup_local

    systemctl stop "${SERVICE_NAME}" >/dev/null 2>&1 || true
    systemctl disable "${SERVICE_NAME}" >/dev/null 2>&1 || true
    rm -f "${SERVICE_FILE}" "${CLI_PATH}"
    systemctl daemon-reload

    if [[ "${purge}" == "1" ]]; then
        rm -rf "${INSTALL_DIR}" "${ENV_DIR}"
        log "service, command, application files and data removed"
    else
        log "service and command removed; data remains in ${INSTALL_DIR} and ${ENV_DIR}"
    fi
}

menu() {
    while true; do
        cat <<EOF

Solovey UI
1) status
2) start
3) stop
4) restart
5) log
6) uri
7) admin -show
8) setting -show
9) update
10) doctor
0) exit
EOF
        printf '> '
        read -r choice
        case "${choice}" in
            1) show_status ;;
            2) systemctl_cmd start ;;
            3) systemctl_cmd stop ;;
            4) systemctl_cmd restart ;;
            5) show_log ;;
            6) run_binary uri ;;
            7) run_binary admin -show ;;
            8) run_binary setting -show ;;
            9) run_installer ;;
            10) run_doctor ;;
            0) exit 0 ;;
            *) usage ;;
        esac
    done
}

command="${1:-}"
if [[ $# -gt 0 ]]; then
    shift
fi

case "${command}" in
    "")
        menu
        ;;
    start|stop|restart|enable|disable)
        systemctl_cmd "${command}"
        ;;
    status)
        show_status
        ;;
    log)
        show_log
        ;;
    uri|admin|setting|migrate|import-xui|decrypt-backup)
        run_binary "${command}" "$@"
        ;;
    build-info)
        print_build_info
        ;;
    version|-v|--version)
        show_version
        ;;
    install|update)
        run_installer "$@"
        ;;
    migrate-from-sui)
        run_installer --migrate-from-sui "$@"
        ;;
    doctor|check)
        run_doctor
        ;;
    backup)
        backup_local
        ;;
    rollback)
        rollback_backup "$@"
        ;;
    uninstall)
        uninstall "$@"
        ;;
    help|-h|--help)
        usage
        ;;
    *)
        run_binary "${command}" "$@"
        ;;
esac
