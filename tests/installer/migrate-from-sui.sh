#!/usr/bin/env bash

set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

FAKEBIN="${TMP}/fakebin"
FIXTURE="${TMP}/fixture"
LOG_DIR="${TMP}/logs"
TARGET="${TMP}/target"
LEGACY="${TMP}/legacy"

mkdir -p "${FAKEBIN}" "${FIXTURE}" "${LOG_DIR}" "${TARGET}" "${LEGACY}"

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
case "${url}" in
    *.tar.gz.sha256) cp "${FIXTURE_SHA}" "${out}" ;;
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
        exit 0
        ;;
    is-active)
        service="${*: -1}"
        case "${service}" in
            s-ui|sing-box) exit 0 ;;
            *) exit 3 ;;
        esac
        ;;
    *)
        exit 0
        ;;
esac
SH

    cat > "${FAKEBIN}/sqlite3" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail

printf '%s\n' "$*" >> "${TEST_INSTALLER_LOG}/sqlite3.args"
cat >> "${TEST_INSTALLER_LOG}/sqlite3.sql"
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

    chmod +x "${FAKEBIN}/curl" "${FAKEBIN}/systemctl" "${FAKEBIN}/sqlite3" "${FAKEBIN}/ln"
}

create_release_fixture() {
    local release_dir="${FIXTURE}/release/solovey-ui"
    local artifact="${FIXTURE}/solovey-ui-linux-amd64.tar.gz"

    mkdir -p "${release_dir}"
    cat > "${release_dir}/solovey-ui" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
printf '%s\n' "$*" >> "${TEST_INSTALLER_LOG}/binary.log"
exit 0
SH
    cat > "${release_dir}/solovey-ui.sh" <<'SH'
#!/usr/bin/env bash
echo "fake manager"
SH
    cp "${ROOT}/solovey-ui.service" "${release_dir}/solovey-ui.service"
    chmod +x "${release_dir}/solovey-ui" "${release_dir}/solovey-ui.sh"

    tar -czf "${artifact}" -C "${FIXTURE}/release" solovey-ui
    (cd "${FIXTURE}" && sha256sum "$(basename "${artifact}")" > "$(basename "${artifact}").sha256")
}

create_legacy_fixture() {
    local legacy_dir="${LEGACY}/usr/local/s-ui"
    local legacy_env="${LEGACY}/etc/s-ui"
    local legacy_systemd="${LEGACY}/etc/systemd/system"

    mkdir -p "${legacy_dir}/db" "${legacy_dir}/cert" "${legacy_dir}/bin" "${legacy_env}" "${legacy_systemd}/s-ui.service.d"
    printf 'legacy db with /usr/local/s-ui/cert/fullchain.pem\n' > "${legacy_dir}/db/s-ui.db"
    printf 'legacy wal\n' > "${legacy_dir}/db/s-ui.db-wal"
    printf 'legacy shm\n' > "${legacy_dir}/db/s-ui.db-shm"
    printf 'legacy cert\n' > "${legacy_dir}/cert/fullchain.pem"
    printf '#!/usr/bin/env sh\nexit 0\n' > "${legacy_dir}/bin/sing-box"
    chmod +x "${legacy_dir}/bin/sing-box"
    printf 'SUI_SECRETBOX_KEY=legacy-secretbox\nSUI_COOKIE_KEY=legacy-cookie\n' > "${legacy_env}/secretbox.env"
    printf '[Service]\nExecStart=/usr/local/s-ui/sui\n' > "${legacy_systemd}/s-ui.service"
    printf '[Service]\nEnvironmentFile=-/etc/s-ui/secretbox.env\n' > "${legacy_systemd}/s-ui.service.d/10-secretbox-env.conf"
}

run_installer() {
    PATH="${FAKEBIN}:${PATH}" \
    FIXTURE_TAR="${FIXTURE}/solovey-ui-linux-amd64.tar.gz" \
    FIXTURE_SHA="${FIXTURE}/solovey-ui-linux-amd64.tar.gz.sha256" \
    TEST_INSTALLER_LOG="${LOG_DIR}" \
    SOLOVEY_UI_ALLOW_NON_ROOT=1 \
    SOLOVEY_UI_GITHUB_RELEASES="https://example.invalid/releases/download" \
    SOLOVEY_UI_INSTALL_DIR="${TARGET}/usr/local/solovey-ui" \
    SOLOVEY_UI_CLI_PATH="${TARGET}/usr/bin/solovey-ui" \
    SOLOVEY_UI_SYSTEMD_SERVICE="${TARGET}/etc/systemd/system/solovey-ui.service" \
    SOLOVEY_UI_ENV_DIR="${TARGET}/etc/solovey-ui" \
    SOLOVEY_UI_BACKUP_ROOT="${TARGET}/var/backups/solovey-ui" \
    SOLOVEY_UI_LEGACY_DIR="${LEGACY}/usr/local/s-ui" \
    SOLOVEY_UI_LEGACY_ENV_DIR="${LEGACY}/etc/s-ui" \
    SOLOVEY_UI_LEGACY_SERVICE_FILE="${LEGACY}/etc/systemd/system/s-ui.service" \
    SOLOVEY_UI_LEGACY_DROPIN_DIR="${LEGACY}/etc/systemd/system/s-ui.service.d" \
    "${BASH:-bash}" "${ROOT}/install.sh" --version v1.5.7-solovey.1 --migrate-from-sui "$@"
}

assert_migrated_install() {
    local install_dir="${TARGET}/usr/local/solovey-ui"
    local target_db="${install_dir}/db/solovey-ui.db"
    local backup_dir

    assert_file "${target_db}"
    cmp "${LEGACY}/usr/local/s-ui/db/s-ui.db" "${target_db}" >/dev/null || fail "target DB differs from legacy DB"
    cmp "${LEGACY}/usr/local/s-ui/db/s-ui.db-wal" "${target_db}-wal" >/dev/null || fail "target WAL differs from legacy WAL"
    cmp "${LEGACY}/usr/local/s-ui/db/s-ui.db-shm" "${target_db}-shm" >/dev/null || fail "target SHM differs from legacy SHM"

    assert_file "${install_dir}/cert/fullchain.pem"
    assert_file "${TARGET}/etc/systemd/system/solovey-ui.service"
    assert_file "${TARGET}/usr/bin/solovey-ui"
    assert_contains "${TARGET}/etc/solovey-ui/secretbox.env" '^SUI_SECRETBOX_KEY=legacy-secretbox$'
    assert_contains "${TARGET}/etc/solovey-ui/secretbox.env" '^SUI_COOKIE_KEY=legacy-cookie$'

    assert_contains "${LOG_DIR}/systemctl.log" '^stop s-ui$'
    assert_contains "${LOG_DIR}/systemctl.log" '^disable s-ui$'
    assert_contains "${LOG_DIR}/systemctl.log" '^stop sing-box$'
    assert_contains "${LOG_DIR}/systemctl.log" '^restart solovey-ui$'
    assert_contains "${LOG_DIR}/binary.log" '^migrate$'
    assert_contains "${LOG_DIR}/sqlite3.args" "${target_db}"
    assert_contains "${LOG_DIR}/sqlite3.sql" "replace\\(value, '/usr/local/s-ui/', '/usr/local/solovey-ui/'\\)"

    backup_dir="$(find "${TARGET}/var/backups/solovey-ui" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
    [[ -n "${backup_dir}" ]] || fail "backup directory was not created"
    assert_file "${backup_dir}/legacy-app/db/s-ui.db"
    assert_file "${backup_dir}/legacy-etc/secretbox.env"
    assert_file "${backup_dir}/s-ui.service"
    assert_file "${backup_dir}/s-ui.service.d/10-secretbox-env.conf"
}

assert_existing_db_is_protected() {
    local output="${LOG_DIR}/refuse-existing-db.out"

    if run_installer >"${output}" 2>&1; then
        fail "installer replaced an existing target DB without --force-migrate"
    fi
    assert_contains "${output}" 'target DB already exists'
}

write_fake_tools
create_release_fixture
create_legacy_fixture

run_installer
assert_migrated_install
assert_existing_db_is_protected

printf 'PASS: installer migrate-from-sui integration\n'
