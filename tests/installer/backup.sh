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

assert_file() {
    [[ -e "$1" ]] || fail "expected file to exist: $1"
}

assert_contains() {
    local file="$1"
    local pattern="$2"
    grep -Eq "${pattern}" "${file}" || fail "expected ${file} to match ${pattern}"
}

assert_backup_dir_count() {
    local expected="$1"
    local actual="0"

    if [[ -d "${BACKUP_ROOT}" ]]; then
        actual="$(find "${BACKUP_ROOT}" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')"
    fi
    [[ "${actual}" == "${expected}" ]] || fail "expected ${expected} backup dir(s), got ${actual}"
}

write_fake_tools() {
    cat > "${FAKEBIN}/df" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "${1:-}" == "-k" ]]; then
    target="${2:-/}"
    printf 'Filesystem 1K-blocks Used Available Use%% Mounted on\n'
    if [[ "${TEST_BACKUP_LOW_SPACE:-0}" == "1" ]]; then
        printf 'fakefs 100 99 1 99%% %s\n' "${target}"
    else
        printf 'fakefs 1000000 1 999999 1%% %s\n' "${target}"
    fi
    exit 0
fi

exec /usr/bin/df "$@"
SH
    chmod +x "${FAKEBIN}/df"
}

create_current_install() {
    printf 'current db\n' > "${INSTALL_DIR}/db/solovey-ui.db"
    printf 'SUI_SECRETBOX_KEY=current-secret\n' > "${ENV_DIR}/secretbox.env"
    printf 'current service\n' > "${SERVICE_FILE}"
    printf '#!/usr/bin/env bash\necho current manager\n' > "${INSTALL_DIR}/solovey-ui.sh"
    printf '#!/usr/bin/env bash\necho current binary\n' > "${INSTALL_DIR}/solovey-ui"
    cp "${INSTALL_DIR}/solovey-ui.sh" "${CLI_PATH}"
    chmod +x "${INSTALL_DIR}/solovey-ui" "${INSTALL_DIR}/solovey-ui.sh" "${CLI_PATH}"
    {
        printf 'app=solovey-ui\n'
        printf 'version=current\n'
        printf 'sing_box=v-current\n'
    } > "${INSTALL_DIR}/BUILD_INFO.txt"
}

run_backup() {
    local name="$1"

    PATH="${FAKEBIN}:${PATH}" \
    SOLOVEY_UI_ALLOW_NON_ROOT=1 \
    SOLOVEY_UI_INSTALL_DIR="${INSTALL_DIR}" \
    SOLOVEY_UI_CLI_PATH="${CLI_PATH}" \
    SOLOVEY_UI_SYSTEMD_SERVICE="${SERVICE_FILE}" \
    SOLOVEY_UI_ENV_DIR="${ENV_DIR}" \
    SOLOVEY_UI_BACKUP_ROOT="${BACKUP_ROOT}" \
    "${BASH:-bash}" "${ROOT}/solovey-ui.sh" backup > "${LOG_DIR}/${name}.out" 2>&1
}

assert_successful_backup() {
    local backup_dir

    assert_backup_dir_count 1
    backup_dir="$(find "${BACKUP_ROOT}" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
    assert_file "${backup_dir}/app/db/solovey-ui.db"
    assert_file "${backup_dir}/etc/secretbox.env"
    assert_file "${backup_dir}/solovey-ui.service"
    assert_contains "${backup_dir}/manifest.txt" '^app=solovey-ui$'
    assert_contains "${backup_dir}/manifest.txt" '^build_version=current$'
    assert_contains "${backup_dir}/manifest.txt" '^build_sing_box=v-current$'
}

write_fake_tools
create_current_install
run_backup success
assert_successful_backup

rm -rf "${BACKUP_ROOT}"
if TEST_BACKUP_LOW_SPACE=1 run_backup low-space; then
    fail "backup should fail when free space is too low"
fi
assert_contains "${LOG_DIR}/low-space.out" 'not enough disk space for backup'
assert_backup_dir_count 0

printf 'PASS: installer backup integration\n'
