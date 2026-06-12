#!/usr/bin/env bash

set -Eeuo pipefail

APP_NAME="solovey-ui"
REPO="${SOLOVEY_UI_REPO:-MalenkiySolovey/solovey-ui}"
INSTALL_URL="${SOLOVEY_UI_INSTALL_URL:-https://raw.githubusercontent.com/${REPO}/main/install.sh}"
VERSION="${SOLOVEY_UI_SMOKE_VERSION:-}"
PURGE_AFTER="${SOLOVEY_UI_SMOKE_PURGE_AFTER:-0}"
CONFIRM="${SOLOVEY_UI_SMOKE_CONFIRM:-}"

usage() {
    cat <<EOF
Run a Solovey UI release smoke test on a disposable Debian-like server.

This script installs the GitHub release, runs post-install checks, and optionally
purges the install afterwards. It is intended for a fresh VM, not production.

Required safety confirmation:
  export SOLOVEY_UI_SMOKE_CONFIRM=disposable

Optional environment:
  SOLOVEY_UI_REPO=MalenkiySolovey/solovey-ui
  SOLOVEY_UI_SMOKE_VERSION=v1.5.7-solovey.1
  SOLOVEY_UI_SMOKE_PURGE_AFTER=1

Usage:
  sudo -E bash scripts/server-smoke-debian.sh
EOF
}

log() {
    printf '[server-smoke] %s\n' "$*"
}

fail() {
    printf '[server-smoke] ERROR: %s\n' "$*" >&2
    exit 1
}

require_root() {
    [[ "${EUID}" -eq 0 ]] || fail "run as root: sudo -E bash scripts/server-smoke-debian.sh"
}

require_disposable_confirmation() {
    [[ "${CONFIRM}" == "disposable" ]] || fail "set SOLOVEY_UI_SMOKE_CONFIRM=disposable on a fresh disposable VM"
}

require_command() {
    command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

install_release() {
    local tmp args

    tmp="$(mktemp)"
    trap 'rm -f "${tmp:-}"' EXIT

    log "downloading installer from ${INSTALL_URL}"
    curl -fsSL --proto '=https' --tlsv1.2 -o "${tmp}" "${INSTALL_URL}"

    args=(--non-interactive)
    if [[ -n "${VERSION}" ]]; then
        args+=(--version "${VERSION}")
    fi

    log "running installer"
    bash "${tmp}" "${args[@]}"
}

run_checks() {
    log "running doctor"
    "${APP_NAME}" doctor

    log "installed version"
    "${APP_NAME}" version

    log "build metadata"
    "${APP_NAME}" build-info

    log "panel URI"
    "${APP_NAME}" uri

    log "systemd status"
    systemctl status "${APP_NAME}" -l --no-pager
}

maybe_purge() {
    if [[ "${PURGE_AFTER}" != "1" ]]; then
        log "leaving install in place for browser/manual checks"
        log "cleanup command: sudo ${APP_NAME} uninstall --purge"
        return 0
    fi

    log "purging smoke install"
    "${APP_NAME}" uninstall --purge
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
    usage
    exit 0
fi

require_root
require_disposable_confirmation
require_command curl
require_command systemctl

install_release
run_checks
maybe_purge

log "smoke test complete"
