#!/usr/bin/env bash

set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

FAKEBIN="${TMP}/fakebin"
TARGET="${TMP}/target"
LOG_DIR="${TMP}/logs"
INSTALL_DIR="${TARGET}/usr/local/solovey-ui"
ENV_DIR="${TARGET}/etc/solovey-ui"
SERVICE_FILE="${TARGET}/etc/systemd/system/solovey-ui.service"
CLI_PATH="${TARGET}/usr/bin/solovey-ui"
BACKUP_ROOT="${TARGET}/var/backups/solovey-ui"

mkdir -p "${FAKEBIN}" "${LOG_DIR}" "${INSTALL_DIR}/db" "${ENV_DIR}" "$(dirname "${SERVICE_FILE}")" "$(dirname "${CLI_PATH}")"

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_contains() {
    local file="$1"
    local pattern="$2"
    grep -Eq "${pattern}" "${file}" || fail "expected ${file} to match ${pattern}"
}

link_tool_if_available() {
    local name="$1"
    local path

    path="$(command -v "${name}" 2>/dev/null || true)"
    [[ -n "${path}" ]] || return 0
    ln -sf "${path}" "${FAKEBIN}/${name}"
}

write_fake_tools() {
    local tool
    for tool in bash sed head grep stat date uname uptime df free; do
        link_tool_if_available "${tool}"
    done

    cat > "${FAKEBIN}/systemctl" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail

printf '%s\n' "$*" >> "${TEST_INSTALLER_LOG}/systemctl.log"
case "${1:-}" in
    is-active)
        exit 0
        ;;
    show)
        printf 'Id=solovey-ui.service\n'
        printf 'LoadState=loaded\n'
        printf 'ActiveState=active\n'
        printf 'SubState=running\n'
        printf 'UnitFileState=enabled\n'
        printf 'ExecMainStatus=0\n'
        printf 'NRestarts=0\n'
        printf 'RestartUSec=100ms\n'
        printf 'FragmentPath=%s\n' "${TEST_SERVICE_FILE}"
        exit 0
        ;;
    *)
        exit 0
        ;;
esac
SH

    cat > "${FAKEBIN}/journalctl" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'fake journal line\n'
SH

    cat > "${FAKEBIN}/curl" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'HTTP/2 200\n'
SH

    cat > "${FAKEBIN}/ip" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
printf 'lo UNKNOWN 127.0.0.1/8\n'
SH

    cat > "${FAKEBIN}/getent" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
printf '140.82.112.4 github.com\n'
SH

    chmod +x "${FAKEBIN}/systemctl" "${FAKEBIN}/journalctl" "${FAKEBIN}/curl" "${FAKEBIN}/ip" "${FAKEBIN}/getent"
}

create_current_install() {
    printf 'not a real sqlite database\n' > "${INSTALL_DIR}/db/solovey-ui.db"
    printf 'SUI_SECRETBOX_KEY=current-secret\n' > "${ENV_DIR}/secretbox.env"
    printf 'current service\n' > "${SERVICE_FILE}"
    cat > "${INSTALL_DIR}/solovey-ui" <<'SH'
#!/usr/bin/env bash
printf 'binary:%s\n' "$*"
SH
    printf '#!/usr/bin/env bash\necho current manager\n' > "${INSTALL_DIR}/solovey-ui.sh"
    cp "${INSTALL_DIR}/solovey-ui.sh" "${CLI_PATH}"
    {
        printf 'app=solovey-ui\n'
        printf 'version=current\n'
        printf 'sing_box=v-current\n'
    } > "${INSTALL_DIR}/BUILD_INFO.txt"
    chmod +x "${INSTALL_DIR}/solovey-ui" "${INSTALL_DIR}/solovey-ui.sh" "${CLI_PATH}"
}

run_command() {
    local name="$1"
    shift

    if ! PATH="${FAKEBIN}:${PATH}" \
    TEST_INSTALLER_LOG="${LOG_DIR}" \
    TEST_SERVICE_FILE="${SERVICE_FILE}" \
    SOLOVEY_UI_ALLOW_NON_ROOT=1 \
    SOLOVEY_UI_INSTALL_DIR="${INSTALL_DIR}" \
    SOLOVEY_UI_CLI_PATH="${CLI_PATH}" \
    SOLOVEY_UI_SYSTEMD_SERVICE="${SERVICE_FILE}" \
    SOLOVEY_UI_ENV_DIR="${ENV_DIR}" \
    SOLOVEY_UI_BACKUP_ROOT="${BACKUP_ROOT}" \
    "${BASH:-bash}" "${ROOT}/solovey-ui.sh" "$@" > "${LOG_DIR}/${name}.out" 2>&1; then
        printf 'command failed: %s\n' "$*" >&2
        sed 's/^/  /' "${LOG_DIR}/${name}.out" >&2
        return 1
    fi
}

assert_full_report() {
    local file="$1"

    assert_contains "${file}" '^== system ==$'
    assert_contains "${file}" '^== binary version ==$'
    assert_contains "${file}" '^== database ==$'
    assert_contains "${file}" '^== panel settings ==$'
    assert_contains "${file}" '^== systemd service ==$'
    assert_contains "${file}" '^== listening ports ==$'
    assert_contains "${file}" '^== network basics ==$'
    assert_contains "${file}" '^== recent warnings/errors ==$'
    assert_contains "${file}" 'sqlite3 not found; DB quick_check and counters skipped'
    assert_contains "${file}" 'settings report skipped: sqlite3/database unavailable'
    assert_contains "${file}" 'ss not found; listening port checks skipped'
    assert_contains "${file}" '^\[solovey-ui\] doctor checks passed$'
}

write_fake_tools
create_current_install

run_command doctor-full doctor --full
run_command diagnose diagnose
run_command report report
run_command ip-cert ip-cert status

assert_full_report "${LOG_DIR}/doctor-full.out"
assert_full_report "${LOG_DIR}/diagnose.out"
assert_full_report "${LOG_DIR}/report.out"
assert_contains "${LOG_DIR}/ip-cert.out" '^binary:ip-cert status$'
assert_contains "${LOG_DIR}/systemctl.log" '^is-active --quiet solovey-ui$'
assert_contains "${LOG_DIR}/systemctl.log" '^show solovey-ui '

printf 'PASS: installer doctor integration\n'
