#!/usr/bin/env bash

set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

FAKEBIN="${TMP}/fakebin"
FIXTURE="${TMP}/fixture"
LOG_DIR="${TMP}/logs"
TARGET="${TMP}/target"
INSTALL_DIR="${TARGET}/usr/local/solovey-ui"
ENV_DIR="${TARGET}/etc/solovey-ui"
SERVICE_FILE="${TARGET}/etc/systemd/system/solovey-ui.service"
CLI_PATH="${TARGET}/usr/bin/solovey-ui"
BACKUP_ROOT="${TARGET}/var/backups/solovey-ui"

mkdir -p "${FAKEBIN}" "${FIXTURE}" "${LOG_DIR}" "${TARGET}"

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_file() {
    [[ -e "$1" ]] || fail "expected file to exist: $1"
}

assert_not_exists() {
    [[ ! -e "$1" ]] || fail "expected file to be absent: $1"
}

assert_contains() {
    local file="$1"
    local pattern="$2"
    grep -Eq "${pattern}" "${file}" || fail "expected ${file} to match ${pattern}"
}

assert_no_backup_dirs() {
    if [[ -d "${BACKUP_ROOT}" ]] && find "${BACKUP_ROOT}" -mindepth 1 -maxdepth 1 -type d | grep -q .; then
        fail "unexpected backup directory for fresh install"
    fi
}

reset_logs() {
    rm -rf "${LOG_DIR}"
    mkdir -p "${LOG_DIR}"
}

write_fake_tools() {
    cat > "${FAKEBIN}/curl" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail

out=""
url=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        -o)
            out="$2"
            shift 2
            ;;
        --proto|-H|-A)
            shift 2
            ;;
        -f|-L|-s|-S|-fL|-fsSL|--tlsv1.2)
            shift
            ;;
        -*)
            shift
            ;;
        *)
            url="$1"
            shift
            ;;
    esac
done

[[ -n "${out}" ]] || { echo "fake curl requires -o" >&2; exit 2; }
if [[ "${TEST_CURL_FAIL:-}" == "artifact" && "${url}" == *.tar.gz ]]; then
    echo "forced artifact download failure" >&2
    exit 22
fi
if [[ "${TEST_CURL_FAIL:-}" == "checksum" && "${url}" == *.tar.gz.sha256 ]]; then
    echo "forced checksum download failure" >&2
    exit 22
fi

case "${url}" in
    *.tar.gz.sha256)
        if [[ "${TEST_BAD_CHECKSUM:-0}" == "1" ]]; then
            printf '0000000000000000000000000000000000000000000000000000000000000000  %s\n' "$(basename "${FIXTURE_TAR}")" > "${out}"
        else
            cp "${FIXTURE_SHA}" "${out}"
        fi
        ;;
    *.tar.gz) cp "${FIXTURE_TAR}" "${out}" ;;
    *) echo "unexpected fake curl URL: ${url}" >&2; exit 3 ;;
esac
SH

    cat > "${FAKEBIN}/systemctl" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail

printf '%s\n' "$*" >> "${TEST_INSTALLER_LOG}/systemctl.log"
case "${1:-}" in
    list-unit-files)
        [[ -f "${TEST_SERVICE_FILE}" ]] && exit 0
        exit 1
        ;;
    is-active)
        [[ -f "${TEST_SERVICE_FILE}" ]] && exit 0
        exit 3
        ;;
    *)
        exit 0
        ;;
esac
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

    cat > "${FAKEBIN}/cp" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "${TEST_FAIL_INSTALL_RESTORE_CP:-0}" == "1" && "${1:-}" == "-a" && "${2:-}" == *"/app" && "${3:-}" == *"/.solovey-ui.restoring."* ]]; then
    echo "simulated install rollback restore copy failure" >&2
    exit 43
fi

exec /usr/bin/cp "$@"
SH

    chmod +x "${FAKEBIN}/curl" "${FAKEBIN}/systemctl" "${FAKEBIN}/ln" "${FAKEBIN}/df" "${FAKEBIN}/cp"
}

create_release_fixture() {
    local version="$1"
    local release_root="${FIXTURE}/${version}"
    local release_dir="${release_root}/solovey-ui"
    local artifact="${release_root}/solovey-ui-linux-amd64.tar.gz"

    rm -rf "${release_root}"
    mkdir -p "${release_dir}"
    cat > "${release_dir}/solovey-ui" <<SH
#!/usr/bin/env bash
set -Eeuo pipefail
printf '%s\n' "${version}:\$*" >> "\${TEST_INSTALLER_LOG}/binary.log"
if [[ "\${TEST_BINARY_FAIL_MIGRATE:-0}" == "1" && "\$*" == "migrate" ]]; then
    exit 42
fi
exit 0
SH
    cat > "${release_dir}/solovey-ui.sh" <<SH
#!/usr/bin/env bash
echo "manager ${version}"
SH
    {
        printf 'app=solovey-ui\n'
        printf 'version=%s\n' "${version}"
        printf 'sing_box=v-test-%s\n' "${version}"
    } > "${release_dir}/BUILD_INFO.txt"
    printf 'service %s\n' "${version}" > "${release_dir}/solovey-ui.service"
    chmod +x "${release_dir}/solovey-ui" "${release_dir}/solovey-ui.sh"

    tar -czf "${artifact}" -C "${release_root}" solovey-ui
    (cd "${release_root}" && sha256sum "$(basename "${artifact}")" > "$(basename "${artifact}").sha256")
}

run_installer() {
    local version="$1"
    shift

    PATH="${FAKEBIN}:${PATH}" \
    FIXTURE_TAR="${FIXTURE}/${version}/solovey-ui-linux-amd64.tar.gz" \
    FIXTURE_SHA="${FIXTURE}/${version}/solovey-ui-linux-amd64.tar.gz.sha256" \
    TEST_INSTALLER_LOG="${LOG_DIR}" \
    TEST_BINARY_FAIL_MIGRATE="${TEST_BINARY_FAIL_MIGRATE:-0}" \
    TEST_BACKUP_LOW_SPACE="${TEST_BACKUP_LOW_SPACE:-0}" \
    TEST_FAIL_INSTALL_RESTORE_CP="${TEST_FAIL_INSTALL_RESTORE_CP:-0}" \
    TEST_SERVICE_FILE="${SERVICE_FILE}" \
    SOLOVEY_UI_ALLOW_NON_ROOT=1 \
    SOLOVEY_UI_GITHUB_RELEASES="https://example.invalid/releases/download" \
    SOLOVEY_UI_INSTALL_DIR="${INSTALL_DIR}" \
    SOLOVEY_UI_CLI_PATH="${CLI_PATH}" \
    SOLOVEY_UI_SYSTEMD_SERVICE="${SERVICE_FILE}" \
    SOLOVEY_UI_ENV_DIR="${ENV_DIR}" \
    SOLOVEY_UI_BACKUP_ROOT="${BACKUP_ROOT}" \
    "${BASH:-bash}" "${ROOT}/install.sh" --version "${version}" "$@"
}

assert_fresh_install() {
    assert_contains "${INSTALL_DIR}/solovey-ui.sh" 'manager v1'
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^version=v1$'
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^sing_box=v-test-v1$'
    assert_contains "${SERVICE_FILE}" '^service v1$'
    assert_contains "${CLI_PATH}" 'manager v1'
    assert_contains "${ENV_DIR}/secretbox.env" '^SUI_SECRETBOX_KEY='
    assert_contains "${LOG_DIR}/binary.log" '^v1:migrate$'
    assert_contains "${LOG_DIR}/systemctl.log" '^enable solovey-ui$'
    assert_contains "${LOG_DIR}/systemctl.log" '^restart solovey-ui$'
    assert_no_backup_dirs
}

assert_update_install() {
    assert_contains "${INSTALL_DIR}/solovey-ui.sh" 'manager v2'
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^version=v2$'
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^sing_box=v-test-v2$'
    assert_contains "${SERVICE_FILE}" '^service v2$'
    assert_contains "${CLI_PATH}" 'manager v2'
    assert_contains "${INSTALL_DIR}/db/solovey-ui.db" '^db after v1$'
    assert_contains "${ENV_DIR}/secretbox.env" '^SUI_SECRETBOX_KEY=existing-secret$'
    assert_contains "${LOG_DIR}/binary.log" '^v2:migrate$'
    assert_contains "${LOG_DIR}/systemctl.log" '^stop solovey-ui$'
    assert_contains "${LOG_DIR}/systemctl.log" '^restart solovey-ui$'

    local backup_dir
    backup_dir="$(find "${BACKUP_ROOT}" -mindepth 1 -maxdepth 1 -type d | sort | head -n 1)"
    [[ -n "${backup_dir}" ]] || fail "update did not create a backup"
    assert_contains "${backup_dir}/app/solovey-ui.sh" 'manager v1'
    assert_contains "${backup_dir}/app/BUILD_INFO.txt" '^version=v1$'
    assert_contains "${backup_dir}/app/db/solovey-ui.db" '^db after v1$'
    assert_contains "${backup_dir}/etc/secretbox.env" '^SUI_SECRETBOX_KEY=existing-secret$'
    assert_contains "${backup_dir}/solovey-ui.service" '^service v1$'
    assert_file "${backup_dir}/manifest.txt"
    assert_contains "${backup_dir}/manifest.txt" '^build_version=v1$'
    assert_contains "${backup_dir}/manifest.txt" '^build_sing_box=v-test-v1$'
}

assert_low_space_backup_precheck() {
    local output="${LOG_DIR}/low-space-update.out"
    local before_count after_count

    before_count="$(find "${BACKUP_ROOT}" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')"
    if TEST_BACKUP_LOW_SPACE=1 run_installer v3 >"${output}" 2>&1; then
        fail "installer succeeded despite forced low backup space"
    fi
    after_count="$(find "${BACKUP_ROOT}" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')"

    [[ "${before_count}" == "${after_count}" ]] || fail "low-space precheck created a partial backup"
    assert_contains "${output}" 'not enough disk space for backup'
    assert_contains "${INSTALL_DIR}/solovey-ui.sh" 'manager v2'
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^version=v2$'
    assert_contains "${SERVICE_FILE}" '^service v2$'
    assert_contains "${CLI_PATH}" 'manager v2'
}

assert_failed_update_rolls_back() {
    local output="${LOG_DIR}/failed-update.out"

    printf 'db after v2\n' > "${INSTALL_DIR}/db/solovey-ui.db"
    if TEST_BINARY_FAIL_MIGRATE=1 run_installer v3 >"${output}" 2>&1; then
        fail "installer succeeded despite forced migrate failure"
    fi

    assert_contains "${output}" 'rolling back from'
    assert_contains "${output}" 'rollback after failed install completed'
    assert_contains "${INSTALL_DIR}/solovey-ui.sh" 'manager v2'
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^version=v2$'
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^sing_box=v-test-v2$'
    assert_contains "${SERVICE_FILE}" '^service v2$'
    assert_contains "${CLI_PATH}" 'manager v2'
    assert_contains "${INSTALL_DIR}/db/solovey-ui.db" '^db after v2$'

    local rollback_backup
    rollback_backup="$(grep -R -l '^build_version=v2$' "${BACKUP_ROOT}"/*/manifest.txt | head -n 1)"
    [[ -n "${rollback_backup}" ]] || fail "failed update did not create a v2 rollback manifest"
}

assert_failed_rollback_copy_is_non_destructive() {
    local output="${LOG_DIR}/failed-rollback-copy.out"

    printf 'db before failed rollback copy\n' > "${INSTALL_DIR}/db/solovey-ui.db"
    if TEST_BINARY_FAIL_MIGRATE=1 TEST_FAIL_INSTALL_RESTORE_CP=1 run_installer v3 >"${output}" 2>&1; then
        fail "installer succeeded despite forced rollback restore failure"
    fi

    assert_contains "${output}" 'rollback restore failed while copying'
    assert_contains "${output}" 'rollback after failed install failed'
    assert_contains "${INSTALL_DIR}/solovey-ui.sh" 'manager v3'
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^version=v3$'
    assert_contains "${SERVICE_FILE}" '^service v3$'
    assert_contains "${INSTALL_DIR}/db/solovey-ui.db" '^db before failed rollback copy$'
}

assert_download_failure_is_non_destructive() {
    local output="${LOG_DIR}/download-failure.out"
    if TEST_CURL_FAIL=artifact run_installer v1 >"${output}" 2>&1; then
        fail "installer succeeded despite forced artifact download failure"
    fi
    assert_not_exists "${INSTALL_DIR}"
    assert_not_exists "${SERVICE_FILE}"
}

assert_checksum_failure_is_non_destructive() {
    local output="${LOG_DIR}/checksum-failure.out"
    if TEST_BAD_CHECKSUM=1 run_installer v1 >"${output}" 2>&1; then
        fail "installer succeeded despite bad checksum"
    fi
    assert_not_exists "${INSTALL_DIR}"
    assert_not_exists "${SERVICE_FILE}"
    assert_contains "${output}" 'FAILED|WARNING'
}

write_fake_tools
create_release_fixture v1
create_release_fixture v2
create_release_fixture v3

reset_logs
run_installer v1
assert_fresh_install

printf 'db after v1\n' > "${INSTALL_DIR}/db/solovey-ui.db"
printf 'SUI_SECRETBOX_KEY=existing-secret\n' > "${ENV_DIR}/secretbox.env"

reset_logs
run_installer v2
assert_update_install

reset_logs
assert_low_space_backup_precheck

reset_logs
assert_failed_update_rolls_back

reset_logs
assert_failed_rollback_copy_is_non_destructive

rm -rf "${TARGET}"
mkdir -p "${TARGET}"
reset_logs
assert_download_failure_is_non_destructive

rm -rf "${TARGET}"
mkdir -p "${TARGET}"
reset_logs
assert_checksum_failure_is_non_destructive

printf 'PASS: installer fresh/update/failure integration\n'
