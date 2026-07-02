#!/usr/bin/env bash

set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

SRC="${TMP}/src"
OUT="${TMP}/out"
EXTRACT="${TMP}/extract"

mkdir -p "${SRC}" "${OUT}" "${EXTRACT}"

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

assert_file() {
    [[ -f "$1" ]] || fail "expected file to exist: $1"
}

assert_executable() {
    [[ -x "$1" ]] || fail "expected file to be executable: $1"
}

assert_contains() {
    local file="$1"
    local pattern="$2"
    grep -Eq "${pattern}" "${file}" || fail "expected ${file} to match ${pattern}"
}

write_fixture() {
    cat > "${SRC}/solovey-ui" <<'SH'
#!/usr/bin/env bash
printf 'fake solovey-ui\n'
SH

    cat > "${SRC}/solovey-ui.sh" <<'SH'
#!/usr/bin/env bash
printf 'fake manager\n'
SH

    cat > "${SRC}/solovey-ui.service" <<'SERVICE'
[Unit]
Description=Fake Solovey UI
SERVICE

    cat > "${SRC}/BUILD_INFO.full.txt" <<'INFO'
app=solovey-ui
profile=full
version=v-test
commit=abc123
platform=linux/amd64
go=go version go1.25.0 linux/amd64
sing_box=v1.12.0
INFO

    cat > "${SRC}/BUILD_INFO.core.txt" <<'INFO'
app=solovey-ui
profile=core
version=v-test
commit=abc123
platform=linux/amd64
go=go version go1.25.0 linux/amd64
sing_box=v1.12.0
INFO

    chmod +x "${SRC}/solovey-ui" "${SRC}/solovey-ui.sh"
}

write_component_fixture() {
    mkdir -p "${SRC}/components/telegram/frontend/assets" "${SRC}/components/paid-subscriptions" "${SRC}/components/remote-outbound-subscriptions"

    cat > "${SRC}/components/telegram/component.json" <<'JSON'
{"id":"telegram","name":"Telegram","version":"1","delivery":"in-process"}
JSON
    printf '{"files":["assets/chunk-test.js"]}\n' > "${SRC}/components/telegram/frontend/assets.json"
    printf 'telegram frontend asset\n' > "${SRC}/components/telegram/frontend/assets/chunk-test.js"
    printf 'do not ship source\n' > "${SRC}/components/telegram/component.go"

    cat > "${SRC}/components/paid-subscriptions/component.json" <<'JSON'
{"id":"paid-subscriptions","name":"Paid Subscriptions","version":"1","delivery":"in-process"}
JSON

    cat > "${SRC}/components/remote-outbound-subscriptions/component.json" <<'JSON'
{"id":"remote-outbound-subscriptions","name":"Remote Outbound Subscriptions","version":"1","delivery":"in-process"}
JSON
}

assert_archive_contract() {
    local artifact="${OUT}/solovey-ui-linux-amd64.tar.gz"
    local checksum="${OUT}/solovey-ui-linux-amd64.tar.gz.sha256"

    assert_file "${artifact}"
    assert_file "${checksum}"

    (
        cd "${OUT}"
        sha256sum -c "$(basename "${checksum}")"
    )

    tar -xzf "${artifact}" -C "${EXTRACT}"
    assert_executable "${EXTRACT}/solovey-ui/solovey-ui"
    assert_executable "${EXTRACT}/solovey-ui/solovey-ui.sh"
    assert_file "${EXTRACT}/solovey-ui/solovey-ui.service"
    assert_file "${EXTRACT}/solovey-ui/BUILD_INFO.txt"
    assert_contains "${EXTRACT}/solovey-ui/BUILD_INFO.txt" '^app=solovey-ui$'
    assert_contains "${EXTRACT}/solovey-ui/BUILD_INFO.txt" '^profile=full$'
    assert_contains "${EXTRACT}/solovey-ui/BUILD_INFO.txt" '^version=v-test$'
    assert_contains "${EXTRACT}/solovey-ui/BUILD_INFO.txt" '^platform=linux/amd64$'
    assert_contains "${EXTRACT}/solovey-ui/BUILD_INFO.txt" '^sing_box=v1.12.0$'

    tar -tzf "${artifact}" | sort > "${TMP}/actual-files.txt"
    cat > "${TMP}/expected-files.txt" <<'EOF'
solovey-ui/
solovey-ui/BUILD_INFO.txt
solovey-ui/solovey-ui
solovey-ui/solovey-ui.service
solovey-ui/solovey-ui.sh
EOF
    diff -u "${TMP}/expected-files.txt" "${TMP}/actual-files.txt" || fail "archive file list does not match release contract"
}

assert_core_archive_contract() {
    local artifact="${OUT}/solovey-ui-core-linux-amd64.tar.gz"
    local checksum="${OUT}/solovey-ui-core-linux-amd64.tar.gz.sha256"

    rm -rf "${EXTRACT}"
    mkdir -p "${EXTRACT}"

    assert_file "${artifact}"
    assert_file "${checksum}"

    (
        cd "${OUT}"
        sha256sum -c "$(basename "${checksum}")"
    )

    tar -xzf "${artifact}" -C "${EXTRACT}"
    assert_executable "${EXTRACT}/solovey-ui/solovey-ui"
    assert_executable "${EXTRACT}/solovey-ui/solovey-ui.sh"
    assert_file "${EXTRACT}/solovey-ui/solovey-ui.service"
    assert_file "${EXTRACT}/solovey-ui/BUILD_INFO.txt"
    assert_contains "${EXTRACT}/solovey-ui/BUILD_INFO.txt" '^profile=core$'
}

assert_component_bundle_contract() {
    local artifact="${OUT}/solovey-ui-components.tar.gz"
    local checksum="${OUT}/solovey-ui-components.tar.gz.sha256"

    rm -rf "${EXTRACT}"
    mkdir -p "${EXTRACT}"

    assert_file "${artifact}"
    assert_file "${checksum}"

    (
        cd "${OUT}"
        sha256sum -c "$(basename "${checksum}")"
    )

    tar -xzf "${artifact}" -C "${EXTRACT}"
    assert_file "${EXTRACT}/components/telegram/component.json"
    assert_file "${EXTRACT}/components/telegram/frontend/assets.json"
    assert_file "${EXTRACT}/components/telegram/frontend/assets/chunk-test.js"
    assert_file "${EXTRACT}/components/paid-subscriptions/component.json"
    assert_file "${EXTRACT}/components/remote-outbound-subscriptions/component.json"
    [[ ! -e "${EXTRACT}/components/telegram/component.go" ]] || fail "component bundle shipped Go source"

    tar -tzf "${artifact}" | sort > "${TMP}/actual-component-files.txt"
    cat > "${TMP}/expected-component-files.txt" <<'EOF'
components/
components/paid-subscriptions/
components/paid-subscriptions/component.json
components/remote-outbound-subscriptions/
components/remote-outbound-subscriptions/component.json
components/telegram/
components/telegram/component.json
components/telegram/frontend/
components/telegram/frontend/assets.json
components/telegram/frontend/assets/
components/telegram/frontend/assets/chunk-test.js
EOF
    diff -u "${TMP}/expected-component-files.txt" "${TMP}/actual-component-files.txt" || fail "component bundle file list does not match release contract"
}

write_fixture
write_component_fixture
bash "${ROOT}/scripts/release-package-linux.sh" \
    --target linux-amd64 \
    --binary "${SRC}/solovey-ui" \
    --manager "${SRC}/solovey-ui.sh" \
    --service "${SRC}/solovey-ui.service" \
    --build-info "${SRC}/BUILD_INFO.full.txt" \
    --out-dir "${OUT}" >/dev/null

assert_archive_contract

bash "${ROOT}/scripts/release-package-linux.sh" \
    --target linux-amd64 \
    --binary "${SRC}/solovey-ui" \
    --manager "${SRC}/solovey-ui.sh" \
    --service "${SRC}/solovey-ui.service" \
    --build-info "${SRC}/BUILD_INFO.core.txt" \
    --profile core \
    --out-dir "${OUT}" >/dev/null

assert_core_archive_contract

bash "${ROOT}/scripts/release-package-components.sh" \
    --components-dir "${SRC}/components" \
    --out-dir "${OUT}" >/dev/null

assert_component_bundle_contract

printf 'PASS: release package integration\n'
