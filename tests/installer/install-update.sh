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
OWNER_CONTRACT="${ENV_DIR}/application-owner-contract.json"

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

assert_not_contains() {
    local file="$1"
    local pattern="$2"
    ! grep -Eq "${pattern}" "${file}" || fail "expected ${file} not to match ${pattern}"
}

assert_no_backup_dirs() {
    if [[ -d "${BACKUP_ROOT}" ]] && find "${BACKUP_ROOT}" -mindepth 1 -maxdepth 1 -type d | grep -q .; then
        fail "unexpected backup directory for fresh install"
    fi
}

assert_component_metadata() {
    local binary="$1"
    local remote="$2"
    local telegram="$3"
    local paid="$4"
    local file="${INSTALL_DIR}/components/installed.json"
    local profile="${binary}"

    if [[ "${binary}" == "full" && ( "${remote}" != "true" || "${telegram}" != "true" || "${paid}" != "true" ) ]]; then
        profile="custom"
    fi

    assert_file "${file}"
    assert_contains "${file}" "\"profile\": \"${profile}\""
    assert_contains "${file}" "\"binary\": \"${binary}\""
    assert_component_metadata_entry "${file}" "remote-outbound-subscriptions" "${remote}"
    assert_component_metadata_entry "${file}" "telegram" "${telegram}"
    assert_component_metadata_entry "${file}" "paid-subscriptions" "${paid}"
}

assert_component_metadata_entry() {
    local file="$1"
    local id="$2"
    local installed="$3"

    if [[ "${installed}" == "true" ]]; then
        assert_contains "${file}" "\"id\": \"${id}\", \"delivery\": \"in-process\", \"installed\": true"
    else
        assert_not_contains "${file}" "\"id\": \"${id}\""
    fi
}

assert_component_pack() {
    local id="$1"
    local installed="$2"
    local dir="${INSTALL_DIR}/components/${id}"

    if [[ "${installed}" == "true" ]]; then
        assert_file "${dir}/component.json"
    else
        assert_not_exists "${dir}"
    fi
}

assert_component_packs() {
    local remote="$1"
    local telegram="$2"
    local paid="$3"

    assert_component_pack remote-outbound-subscriptions "${remote}"
    assert_component_pack telegram "${telegram}"
    assert_component_pack paid-subscriptions "${paid}"
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
            printf '0000000000000000000000000000000000000000000000000000000000000000  %s\n' "$(basename "${url%.sha256}")" > "${out}"
        elif [[ "${url}" == *"/solovey-ui-components.tar.gz.sha256" ]]; then
            cp "${FIXTURE_COMPONENTS_SHA}" "${out}"
        elif [[ "${url}" == *"/solovey-ui-core-linux-"*.tar.gz.sha256 ]]; then
            cp "${FIXTURE_CORE_SHA}" "${out}"
        else
            cp "${FIXTURE_SHA}" "${out}"
        fi
        ;;
    *.tar.gz)
        if [[ "${url}" == *"/solovey-ui-components.tar.gz" ]]; then
            cp "${FIXTURE_COMPONENTS_TAR}" "${out}"
        elif [[ "${url}" == *"/solovey-ui-core-linux-"*.tar.gz ]]; then
            cp "${FIXTURE_CORE_TAR}" "${out}"
        else
            cp "${FIXTURE_TAR}" "${out}"
        fi
        ;;
    *) echo "unexpected fake curl URL: ${url}" >&2; exit 3 ;;
esac
SH

    cat > "${FAKEBIN}/systemctl" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail

printf '%s\n' "$*" >> "${TEST_INSTALLER_LOG}/systemctl.log"
case "${1:-}" in
    --version)
		echo "systemd ${TEST_SYSTEMD_VERSION:-255} (${TEST_SYSTEMD_VERSION:-255}.1-test)"
        exit 0
        ;;
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

	for tool in systemd-sysusers systemd-tmpfiles chown; do
		cat > "${FAKEBIN}/${tool}" <<'SH'
#!/usr/bin/env bash
exit 0
SH
	done
	cat > "${FAKEBIN}/runuser" <<'SH'
#!/usr/bin/env bash
set -Eeuo pipefail
while [[ $# -gt 0 && "$1" != "--" ]]; do shift; done
[[ "${1:-}" == "--" ]] && shift
exec "$@"
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

    chmod +x "${FAKEBIN}/curl" "${FAKEBIN}/systemctl" "${FAKEBIN}/df" "${FAKEBIN}/cp" \
		"${FAKEBIN}/systemd-sysusers" "${FAKEBIN}/systemd-tmpfiles" "${FAKEBIN}/chown" "${FAKEBIN}/runuser"
}

create_release_fixture() {
    local version="$1"
    local release_root="${FIXTURE}/${version}"
    local release_dir="${release_root}/solovey-ui"
    local artifact="${release_root}/solovey-ui-linux-amd64.tar.gz"
    local core_artifact="${release_root}/solovey-ui-core-linux-amd64.tar.gz"
    local components_artifact="${release_root}/solovey-ui-components.tar.gz"

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
        printf 'profile=full\n'
        printf 'version=%s\n' "${version}"
        printf 'sing_box=v-test-%s\n' "${version}"
    } > "${release_dir}/BUILD_INFO.txt"
    printf 'service %s\n' "${version}" > "${release_dir}/solovey-ui.service"
	for name in solovey-privileged-broker solovey-ssh-proof solovey-broker-manifest solovey-owner-manifest; do
		cat > "${release_dir}/${name}" <<SH
#!/usr/bin/env bash
printf '%s\n' "${version}:${name}" >> "\${TEST_INSTALLER_LOG}/binary.log"
exit 0
SH
	done
	mkdir -p "${release_dir}/systemd"
	for unit in solovey-ui-native-hardened.service solovey-ui-native-network-advanced.service solovey-ui-native-legacy-root.service \
		solovey-privileged-broker.service solovey-privileged-broker.socket solovey-privileged-proof.socket solovey-ui.sysusers solovey-ui.tmpfiles; do
		printf 'deployment asset %s %s\n' "${version}" "${unit}" > "${release_dir}/systemd/${unit}"
	done
	chmod +x "${release_dir}/solovey-ui" "${release_dir}/solovey-ui.sh" "${release_dir}/solovey-privileged-broker" \
		"${release_dir}/solovey-ssh-proof" "${release_dir}/solovey-broker-manifest" "${release_dir}/solovey-owner-manifest"

    tar -czf "${artifact}" -C "${release_root}" solovey-ui
    (cd "${release_root}" && sha256sum "$(basename "${artifact}")" > "$(basename "${artifact}").sha256")
    sed -i 's/^profile=full$/profile=core/' "${release_dir}/BUILD_INFO.txt"
	rm -f "${release_dir}/solovey-owner-manifest"
    tar -czf "${core_artifact}" -C "${release_root}" solovey-ui
    (cd "${release_root}" && sha256sum "$(basename "${core_artifact}")" > "$(basename "${core_artifact}").sha256")

    mkdir -p \
        "${release_root}/components/remote-outbound-subscriptions" \
        "${release_root}/components/telegram" \
        "${release_root}/components/paid-subscriptions"
    printf '{"id":"remote-outbound-subscriptions","delivery":"in-process"}\n' > "${release_root}/components/remote-outbound-subscriptions/component.json"
    printf '{"id":"telegram","delivery":"in-process"}\n' > "${release_root}/components/telegram/component.json"
    printf '{"id":"paid-subscriptions","delivery":"in-process"}\n' > "${release_root}/components/paid-subscriptions/component.json"
    tar -czf "${components_artifact}" -C "${release_root}" components
    (cd "${release_root}" && sha256sum "$(basename "${components_artifact}")" > "$(basename "${components_artifact}").sha256")
}

create_bad_component_fixture() {
    local version="$1"
    local release_root="${FIXTURE}/${version}"
    local components_artifact="${release_root}/solovey-ui-components.tar.gz"

    create_release_fixture "${version}"
    printf '{"id":"not-telegram","delivery":"in-process"}\n' > "${release_root}/components/telegram/component.json"
    tar -czf "${components_artifact}" -C "${release_root}" components
    (cd "${release_root}" && sha256sum "$(basename "${components_artifact}")" > "$(basename "${components_artifact}").sha256")
}

run_installer() {
    local version="$1"
    shift

    PATH="${FAKEBIN}:${PATH}" \
    FIXTURE_TAR="${FIXTURE}/${version}/solovey-ui-linux-amd64.tar.gz" \
    FIXTURE_SHA="${FIXTURE}/${version}/solovey-ui-linux-amd64.tar.gz.sha256" \
    FIXTURE_CORE_TAR="${FIXTURE}/${version}/solovey-ui-core-linux-amd64.tar.gz" \
    FIXTURE_CORE_SHA="${FIXTURE}/${version}/solovey-ui-core-linux-amd64.tar.gz.sha256" \
    FIXTURE_COMPONENTS_TAR="${FIXTURE}/${version}/solovey-ui-components.tar.gz" \
    FIXTURE_COMPONENTS_SHA="${FIXTURE}/${version}/solovey-ui-components.tar.gz.sha256" \
    TEST_INSTALLER_LOG="${LOG_DIR}" \
    TEST_BINARY_FAIL_MIGRATE="${TEST_BINARY_FAIL_MIGRATE:-0}" \
    TEST_BACKUP_LOW_SPACE="${TEST_BACKUP_LOW_SPACE:-0}" \
    TEST_FAIL_INSTALL_RESTORE_CP="${TEST_FAIL_INSTALL_RESTORE_CP:-0}" \
	TEST_SYSTEMD_VERSION="${TEST_SYSTEMD_VERSION:-255}" \
    TEST_SERVICE_FILE="${SERVICE_FILE}" \
    SOLOVEY_UI_ALLOW_NON_ROOT=1 \
    SOLOVEY_UI_GITHUB_RELEASES="https://example.invalid/releases/download" \
    SOLOVEY_UI_INSTALL_DIR="${INSTALL_DIR}" \
    SOLOVEY_UI_CLI_PATH="${CLI_PATH}" \
    SOLOVEY_UI_SYSTEMD_SERVICE="${SERVICE_FILE}" \
	SOLOVEY_UI_SYSTEMD_UNIT_ROOT="${TARGET}/etc/systemd/system" \
	SOLOVEY_UI_SYSTEMD_PROFILE_ROOT="${TARGET}/usr/local/lib/solovey-ui/systemd" \
	SOLOVEY_UI_DEPLOYMENT_MARKER="${ENV_DIR}/deployment-profile" \
	SOLOVEY_UI_APPLICATION_OWNER_CONTRACT="${OWNER_CONTRACT}" \
	SOLOVEY_UI_HARDENED_DATA_ROOT="${TARGET}/var/lib/solovey-ui" \
    SOLOVEY_UI_ENV_DIR="${ENV_DIR}" \
    SOLOVEY_UI_BACKUP_ROOT="${BACKUP_ROOT}" \
    "${BASH:-bash}" "${ROOT}/install.sh" --version "${version}" "$@"
}

assert_fresh_install() {
    assert_contains "${INSTALL_DIR}/solovey-ui.sh" 'manager v1'
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^version=v1$'
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^profile=full$'
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^sing_box=v-test-v1$'
	assert_contains "${SERVICE_FILE}" 'solovey-ui-native-hardened.service'
	assert_contains "${ENV_DIR}/deployment-profile" '^native-hardened$'
	for name in solovey-privileged-broker solovey-ssh-proof solovey-broker-manifest solovey-owner-manifest; do
		assert_file "${INSTALL_DIR}/${name}"
	done
	for unit in solovey-privileged-broker.service solovey-privileged-broker.socket solovey-privileged-proof.socket; do
		assert_file "${TARGET}/etc/systemd/system/${unit}"
	done
	assert_contains "${LOG_DIR}/binary.log" '^v1:solovey-broker-manifest$'
	assert_contains "${LOG_DIR}/binary.log" '^v1:solovey-owner-manifest$'
	assert_contains "${LOG_DIR}/systemctl.log" '^enable solovey-privileged-broker.socket solovey-privileged-proof.socket$'
	if [[ "$(uname -s)" == Linux ]]; then
		[[ "$(stat -Lc '%a' "${INSTALL_DIR}/solovey-ssh-proof")" == 2755 ]] || fail "SSH proof helper is not installed setgid 2755"
	fi
    assert_contains "${CLI_PATH}" 'manager v1'
    assert_contains "${ENV_DIR}/secretbox.env" '^SUI_SECRETBOX_KEY='
    assert_contains "${ENV_DIR}/secretbox.env" '^SUI_COOKIE_KEY='
    assert_contains "${LOG_DIR}/binary.log" '^v1:migrate$'
    assert_contains "${LOG_DIR}/systemctl.log" '^enable solovey-ui$'
    assert_contains "${LOG_DIR}/systemctl.log" '^restart solovey-ui$'
    assert_component_metadata full true true true
    assert_component_packs true true true
    assert_no_backup_dirs
}

assert_update_install() {
    assert_contains "${INSTALL_DIR}/solovey-ui.sh" 'manager v2'
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^version=v2$'
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^profile=full$'
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^sing_box=v-test-v2$'
	assert_contains "${SERVICE_FILE}" 'solovey-ui-native-hardened.service'
    assert_contains "${CLI_PATH}" 'manager v2'
    assert_contains "${INSTALL_DIR}/db/solovey-ui.db" '^db after v1$'
    assert_contains "${ENV_DIR}/secretbox.env" '^SUI_SECRETBOX_KEY=existing-secret$'
    assert_contains "${ENV_DIR}/secretbox.env" '^SUI_COOKIE_KEY='
    assert_component_metadata full true true true
    assert_component_packs true true true
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
	assert_contains "${backup_dir}/solovey-ui.service" 'solovey-ui-native-hardened.service'
	assert_contains "${backup_dir}/hardened-data/db/solovey-ui.db" '^hardened db after v1$'
	assert_file "${backup_dir}/systemd-assets/profiles/solovey-ui-native-hardened.service"
	assert_file "${backup_dir}/systemd-assets/units/solovey-privileged-broker.service"
	assert_contains "${backup_dir}/systemd-assets/inventory.txt" '^solovey-privileged-broker.socket=present$'
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
	assert_contains "${SERVICE_FILE}" 'solovey-ui-native-hardened.service'
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
	assert_contains "${SERVICE_FILE}" 'solovey-ui-native-hardened.service'
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
	assert_contains "${SERVICE_FILE}" 'solovey-ui-native-hardened.service'
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

assert_custom_component_selection() {
    rm -rf "${TARGET}"
    mkdir -p "${TARGET}"
    reset_logs

    run_installer v1 --with remote-outbound-subscriptions --without telegram,paid-subscriptions
    assert_component_metadata full true false false
    assert_component_packs true false false
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^profile=full$'
    assert_contains "${LOG_DIR}/binary.log" '^v1:migrate$'
}

assert_auto_core_component_selection() {
    rm -rf "${TARGET}"
    mkdir -p "${ENV_DIR}"
	printf 'stale owner contract\n' > "${OWNER_CONTRACT}"
    reset_logs

    run_installer v1 --without all
    assert_component_metadata core false false false
    assert_component_packs false false false
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^profile=core$'
    assert_contains "${LOG_DIR}/binary.log" '^v1:migrate$'
	assert_not_exists "${INSTALL_DIR}/solovey-owner-manifest"
	assert_not_exists "${OWNER_CONTRACT}"
	assert_not_contains "${LOG_DIR}/binary.log" 'solovey-owner-manifest'
}

assert_minimal_profile_alias() {
    rm -rf "${TARGET}"
    mkdir -p "${TARGET}"
    reset_logs

    run_installer v1 --profile minimal
    assert_component_metadata core false false false
    assert_component_packs false false false
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^profile=core$'
    assert_contains "${LOG_DIR}/binary.log" '^v1:migrate$'
}

assert_explicit_full_without_all_resolves_core_binary() {
    rm -rf "${TARGET}"
    mkdir -p "${TARGET}"
    reset_logs

    run_installer v1 --profile full --without all
    assert_component_metadata core false false false
    assert_component_packs false false false
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^profile=core$'
    assert_contains "${LOG_DIR}/binary.log" '^v1:migrate$'
}

assert_minimal_with_inprocess_component_resolves_full_binary() {
    rm -rf "${TARGET}"
    mkdir -p "${TARGET}"
    reset_logs

    run_installer v1 --profile minimal --with remote-outbound-subscriptions
    assert_component_metadata full true false false
    assert_component_packs true false false
    assert_contains "${INSTALL_DIR}/BUILD_INFO.txt" '^profile=full$'
    assert_contains "${LOG_DIR}/binary.log" '^v1:migrate$'
}

assert_require_core_rejects_inprocess_components() {
    rm -rf "${TARGET}"
    mkdir -p "${TARGET}"
    reset_logs

    local output="${LOG_DIR}/require-core.out"
    if run_installer v1 --profile minimal --with remote-outbound-subscriptions --require-core >"${output}" 2>&1; then
        fail "installer accepted an in-process component with --require-core"
    fi
    assert_not_exists "${INSTALL_DIR}"
    assert_contains "${output}" 'selected components require the full binary'
}

assert_component_pack_removal_on_update() {
    rm -rf "${TARGET}"
    mkdir -p "${TARGET}"
    reset_logs

    run_installer v1
    assert_component_packs true true true

    reset_logs
    run_installer v2 --with remote-outbound-subscriptions --without telegram,paid-subscriptions
    assert_component_metadata full true false false
    assert_component_packs true false false
    assert_contains "${LOG_DIR}/binary.log" '^v2:migrate$'
}

assert_bad_component_pack_is_rejected() {
    rm -rf "${TARGET}"
    mkdir -p "${TARGET}"
    reset_logs

    local output="${LOG_DIR}/bad-component-pack.out"
    if run_installer vbad --with telegram >"${output}" 2>&1; then
        fail "installer accepted a component pack with mismatched id"
    fi
    assert_not_exists "${INSTALL_DIR}"
    assert_contains "${output}" 'component pack id mismatch'
}

assert_unsupported_systemd_is_rejected_before_install() {
	rm -rf "${TARGET}"
	mkdir -p "${TARGET}"
	reset_logs
	local output="${LOG_DIR}/unsupported-systemd.out"
	if TEST_SYSTEMD_VERSION=248 run_installer v1 >"${output}" 2>&1; then
		fail "fresh hardened install accepted unsupported systemd"
	fi
	assert_not_exists "${INSTALL_DIR}"
	assert_not_exists "${SERVICE_FILE}"
	assert_contains "${output}" 'requires systemd 249 or newer'
}

assert_native_advanced_fails_closed_before_install() {
	rm -rf "${TARGET}"
	mkdir -p "${ENV_DIR}"
	printf 'native-network-advanced\n' > "${ENV_DIR}/deployment-profile"
	reset_logs
	local output="${LOG_DIR}/advanced-unavailable.out"
	if run_installer v1 >"${output}" 2>&1; then
		fail "installer activated native advanced without a separated runtime"
	fi
	assert_not_exists "${INSTALL_DIR}"
	assert_not_exists "${SERVICE_FILE}"
	assert_contains "${output}" 'separately confined core runtime'
}

write_fake_tools
create_release_fixture v1
create_release_fixture v2
create_release_fixture v3
create_bad_component_fixture vbad

reset_logs
run_installer v1
assert_fresh_install

printf 'db after v1\n' > "${INSTALL_DIR}/db/solovey-ui.db"
mkdir -p "${TARGET}/var/lib/solovey-ui/db"
printf 'hardened db after v1\n' > "${TARGET}/var/lib/solovey-ui/db/solovey-ui.db"
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

assert_custom_component_selection
assert_auto_core_component_selection
assert_minimal_profile_alias
assert_explicit_full_without_all_resolves_core_binary
assert_minimal_with_inprocess_component_resolves_full_binary
assert_require_core_rejects_inprocess_components
assert_component_pack_removal_on_update
assert_bad_component_pack_is_rejected
assert_unsupported_systemd_is_rejected_before_install
assert_native_advanced_fails_closed_before_install

printf 'PASS: installer fresh/update/failure integration\n'
