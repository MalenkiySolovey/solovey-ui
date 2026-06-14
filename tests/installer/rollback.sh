#!/usr/bin/env bash

set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

FAKEBIN="${TMP}/fakebin"
TARGET="${TMP}/target"
LOG_DIR="${TMP}/logs"
BACKUP_ROOT="${TARGET}/var/backups/solovey-ui"
INSTALL_DIR="${TARGET}/usr/local/solovey-ui"
ENV_DIR="${TARGET}/etc/solovey-ui"
SERVICE_FILE="${TARGET}/etc/systemd/system/solovey-ui.service"
CLI_PATH="${TARGET}/usr/bin/solovey-ui"

mkdir -p "${FAKEBIN}" "${LOG_DIR}" "${INSTALL_DIR}/db" "${ENV_DIR}" "$(dirname "${SERVICE_FILE}")" "$(dirname "${CLI_PATH}")"

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_file() {
    [[ -e "$1" ]] || fail "expected file to exist: $1"
}

assert_contains() {
    local file="$1"
    local pattern="$2"
    grep -Eq "${pattern}" "${file}" || fail "expected ${file} to match ${pattern}"
}

write_fake_tools() {
    cat > "${FAKEBIN}/systemctl" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
printf '%s\n' "$*" >> "${TEST_INSTALLER_LOG}/systemctl.log"
exit 0
SH

    cat > "${FAKEBIN}/ln" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ "${1:-}" == "-sf" ]]; then
    cp "$2" "$3"
    chmod +x "$3"
    exit 0
fi
echo "unexpected fake ln invocation: $*" >&2
exit 2
SH

    cat > "${FAKEBIN}/cp" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ "${TEST_FAIL_RESTORE_CP:-}" == "1" && "${1:-}" == "-a" && "${2:-}" == *"/20260101T000000Z/app" ]]; then
    echo "simulated restore copy failure" >&2
    exit 42
fi
exec /usr/bin/cp "$@"
SH

    chmod +x "${FAKEBIN}/systemctl" "${FAKEBIN}/ln" "${FAKEBIN}/cp"
}

create_current_install() {
    printf 'current db\n' > "${INSTALL_DIR}/db/solovey-ui.db"
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

create_backup() {
    local backup="${BACKUP_ROOT}/20260101T000000Z"

    mkdir -p "${backup}/app/db" "${backup}/etc"
    printf 'restored db\n' > "${backup}/app/db/solovey-ui.db"
    printf '#!/usr/bin/env bash\necho restored manager\n' > "${backup}/app/solovey-ui.sh"
    printf '#!/usr/bin/env bash\necho restored binary\n' > "${backup}/app/solovey-ui"
    chmod +x "${backup}/app/solovey-ui.sh" "${backup}/app/solovey-ui"
    printf 'restored env\n' > "${backup}/etc/secretbox.env"
    printf 'restored service\n' > "${backup}/solovey-ui.service"
    cat > "${backup}/manifest.txt" <<EOF
app=solovey-ui
created_at=20260101T000000Z
install_dir=${INSTALL_DIR}
env_dir=${ENV_DIR}
service=${SERVICE_FILE}
EOF
}

run_rollback() {
    local requested="${1:-latest}"
    PATH="${FAKEBIN}:${PATH}" \
    TEST_INSTALLER_LOG="${LOG_DIR}" \
    TEST_FAIL_RESTORE_CP="${TEST_FAIL_RESTORE_CP:-}" \
    SOLOVEY_UI_ALLOW_NON_ROOT=1 \
    SOLOVEY_UI_INSTALL_DIR="${INSTALL_DIR}" \
    SOLOVEY_UI_CLI_PATH="${CLI_PATH}" \
    SOLOVEY_UI_SYSTEMD_SERVICE="${SERVICE_FILE}" \
    SOLOVEY_UI_ENV_DIR="${ENV_DIR}" \
    SOLOVEY_UI_BACKUP_ROOT="${BACKUP_ROOT}" \
    "${BASH:-bash}" "${ROOT}/solovey-ui.sh" rollback "${requested}"
}

run_version() {
    SOLOVEY_UI_ALLOW_NON_ROOT=1 \
    SOLOVEY_UI_INSTALL_DIR="${INSTALL_DIR}" \
    "${BASH:-bash}" "${ROOT}/solovey-ui.sh" version > "${LOG_DIR}/version.out"
}

run_doctor() {
    PATH="${FAKEBIN}:${PATH}" \
    TEST_INSTALLER_LOG="${LOG_DIR}" \
    SOLOVEY_UI_ALLOW_NON_ROOT=1 \
    SOLOVEY_UI_INSTALL_DIR="${INSTALL_DIR}" \
    SOLOVEY_UI_CLI_PATH="${CLI_PATH}" \
    SOLOVEY_UI_SYSTEMD_SERVICE="${SERVICE_FILE}" \
    SOLOVEY_UI_ENV_DIR="${ENV_DIR}" \
    SOLOVEY_UI_BACKUP_ROOT="${BACKUP_ROOT}" \
    "${BASH:-bash}" "${ROOT}/solovey-ui.sh" doctor > "${LOG_DIR}/doctor.out"
}

assert_version() {
    assert_contains "${LOG_DIR}/version.out" '^binary:-v$'
    assert_contains "${LOG_DIR}/version.out" '^Build metadata:$'
    assert_contains "${LOG_DIR}/version.out" '^version=current$'
    assert_contains "${LOG_DIR}/version.out" '^sing_box=v-current$'
}

assert_doctor() {
    assert_contains "${LOG_DIR}/doctor.out" '^\[OK\] binary:'
    assert_contains "${LOG_DIR}/doctor.out" '^\[OK\] manager script:'
    assert_contains "${LOG_DIR}/doctor.out" '^\[OK\] CLI command:'
    assert_contains "${LOG_DIR}/doctor.out" '^\[OK\] systemd service file:'
    assert_contains "${LOG_DIR}/doctor.out" '^\[OK\] database:'
    assert_contains "${LOG_DIR}/doctor.out" '^\[OK\] secret env contains SUI_SECRETBOX_KEY$'
    assert_contains "${LOG_DIR}/doctor.out" '^\[OK\] build version=current$'
    assert_contains "${LOG_DIR}/doctor.out" '^\[OK\] embedded sing-box=v-current$'
    assert_contains "${LOG_DIR}/doctor.out" '^\[OK\] service active: solovey-ui$'
    assert_contains "${LOG_DIR}/doctor.out" '^\[solovey-ui\] doctor checks passed$'
    assert_contains "${LOG_DIR}/systemctl.log" '^is-active --quiet solovey-ui$'
}

assert_rollback() {
    assert_contains "${INSTALL_DIR}/db/solovey-ui.db" '^restored db$'
    assert_contains "${ENV_DIR}/secretbox.env" '^restored env$'
    assert_contains "${SERVICE_FILE}" '^restored service$'
    assert_contains "${CLI_PATH}" 'restored manager'

    assert_contains "${LOG_DIR}/systemctl.log" '^stop solovey-ui$'
    assert_contains "${LOG_DIR}/systemctl.log" '^daemon-reload$'
    assert_contains "${LOG_DIR}/systemctl.log" '^restart solovey-ui$'

    local safety_backup
    safety_backup="$(find "${BACKUP_ROOT}" -mindepth 1 -maxdepth 1 -type d ! -name '20260101T000000Z' | head -n 1)"
    [[ -n "${safety_backup}" ]] || fail "safety backup was not created"
    assert_file "${safety_backup}/manifest.txt"
    assert_contains "${safety_backup}/manifest.txt" '^build_version=current$'
    assert_contains "${safety_backup}/manifest.txt" '^build_sing_box=v-current$'
    assert_contains "${safety_backup}/app/db/solovey-ui.db" '^current db$'
    assert_contains "${safety_backup}/etc/secretbox.env" '^SUI_SECRETBOX_KEY=current-secret$'
    assert_contains "${safety_backup}/solovey-ui.service" '^current service$'
}

assert_failed_restore_leaves_current_install() {
    if TEST_FAIL_RESTORE_CP=1 run_rollback 20260101T000000Z > "${LOG_DIR}/rollback-fail.out" 2>&1; then
        fail "rollback should fail when restore copy fails"
    fi
    assert_contains "${LOG_DIR}/rollback-fail.out" 'existing .+ was left unchanged'
    assert_contains "${INSTALL_DIR}/db/solovey-ui.db" '^current db$'
    assert_contains "${ENV_DIR}/secretbox.env" '^SUI_SECRETBOX_KEY=current-secret$'
    assert_contains "${SERVICE_FILE}" '^current service$'
    assert_contains "${CLI_PATH}" 'current manager'
}

write_fake_tools
create_current_install
create_backup
run_version
assert_version
run_doctor
assert_doctor
assert_failed_restore_leaves_current_install
run_rollback 20260101T000000Z
assert_rollback

printf 'PASS: installer rollback integration\n'
