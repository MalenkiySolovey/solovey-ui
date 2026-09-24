#!/usr/bin/env bash
set -Eeuo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_ROOT=$(mktemp -d)
trap 'rm -rf "$TEST_ROOT"' EXIT
export SOLOVEY_UI_INSTALL_DIR="$TEST_ROOT/app" SOLOVEY_UI_ENV_DIR="$TEST_ROOT/etc"
export SOLOVEY_UI_BACKUP_ROOT="$TEST_ROOT/backups"
export SOLOVEY_UI_SYSTEMD_SERVICE="$TEST_ROOT/units/solovey-ui.service"
source "${ROOT}/install.sh"

accept() {
    local actual
    actual=$(printf '%s' "$1" | json_string_field tag_name)
    [[ "$actual" == 'v2030.2.4' ]] || { echo 'FAIL: semantic tag mismatch' >&2; exit 1; }
}
reject() {
    local actual
    if actual=$(printf '%s' "$1" | json_string_field tag_name); then
        echo 'FAIL: accepted invalid release JSON' >&2; exit 1
    fi
    [[ -z "$actual" ]] || { echo 'FAIL: emitted identity before complete validation' >&2; exit 1; }
}
accept '{"id":1,"tag_name":"v2030.2.4","assets":[]}'
accept $'{\n  "tag_name": "v2030.2.4",\n  "id": 1\n}'
accept $' \t\r\n{\r"id"\t:\n[true,false,null,-1.5e+2], "tag_name"\r:\t"v2030.2.4"}\n'
accept '{"body":"escaped \"tag_name\":\"wrong\" and \\ and \u2603","nested":{"tag_name":"wrong"},"tag_name":"v2030.2.4"}'
accept '{"tag_\u006eame":"\u00762030.2.4"}'
accept $'{"body":"\x7f \xc2\xa2 \xe2\x98\x83 \xf0\x9f\x98\x80","tag_name":"v2030.2.4"}'
for invalid_utf8 in $'\x80' $'\xc0\xaf' $'\xe0\x80\xaf' $'\xed\xa0\x80' $'\xf4\x90\x80\x80' $'\xf0\x9f'; do
    reject "{\"tag_name\":\"v2030.2.4\",\"body\":\"${invalid_utf8}\"}"
done
for input in '{}' '{"tag_name":null}' '{"tag_name":""}' '{"tag_name":12}' \
    '{"tag_name":true}' '{"tag_name":[]}' '{"tag_name":{}}' \
    '{"nested":{"tag_name":"v2030.2.4"}}' '{"message":"API rate limit exceeded"}' \
    '{"tag_name":"v2030.2.4",}' '{"tag_name":"v2030.2.4"} trailing' \
    '{"tag_name":"v2030.2.4","x":01}' '{"tag_name":"v2030.2.4","x":1.}' \
    '{"tag_name":"v2030.2.4","x":"\q"}' '{"tag_name":"v2030.2.4","x":"\u00xz"}' \
    '{"tag_name":"v2030.2.4","x":[1,]}' '[{"tag_name":"v2030.2.4"}]' \
    '{"tag_name":"v2030.2.4","tag_name":"v2030.2.4"}' \
    '{"tag_name":"v2030.2.4"' $'{"tag_name":"v2030.2.4","x":"raw\nnewline"}'; do
    reject "$input"
done

secure_curl() { printf '%s' '{"id":1,"tag_name":"v2030.2.4"}'; }
[[ "$(latest_version)" == 'v2030.2.4' ]]
secure_curl() { printf '%s' '{"tag_name":"v2030.2.4"}'; return 22; }
if (latest_version) >/dev/null 2>&1; then echo 'FAIL: HTTP failure accepted' >&2; exit 1; fi
secure_curl() { printf '%s' '{"tag_name":"../../unsafe"}'; }
if (latest_version) >/dev/null 2>&1; then echo 'FAIL: invalid release tag accepted' >&2; exit 1; fi

# Both version routes and both binary profiles use the actual installer resolver.
secure_curl() { printf '%s' '{"id":1,"tag_name":"v2030.2.4"}'; }
detect_arch() { echo arm64; }
DRY_RUN=1
BACKUP_MODE=always
for profile in full core; do
    BINARY_PROFILE="$profile"
    for VERSION in '' v2030.2.4; do
        output=$(download_and_install)
        [[ "$output" == *'release: v2030.2.4'* ]]
        [[ "$output" == *"artifact: $(release_artifact_name arm64)"* ]]
    done
done
[[ ! -e "$TEST_ROOT/backups" ]] || { echo 'FAIL: dry-run mutated backup storage' >&2; exit 1; }
(
    cd "$TEST_ROOT"
    printf 'checksum regression fixture\n' > artifact.tar.gz
    sha256sum artifact.tar.gz > artifact.tar.gz.sha256
    verify_release_checksum artifact.tar.gz >/dev/null
    cp artifact.tar.gz other.tar.gz
    sha256sum other.tar.gz > artifact.tar.gz.sha256
    if (verify_release_checksum artifact.tar.gz) >/dev/null 2>&1; then echo 'FAIL: checksum authorized another file' >&2; exit 1; fi
    sha256sum artifact.tar.gz other.tar.gz > artifact.tar.gz.sha256
    if (verify_release_checksum artifact.tar.gz) >/dev/null 2>&1; then echo 'FAIL: multiple checksum entries accepted' >&2; exit 1; fi
    sha256sum artifact.tar.gz > artifact.tar.gz.sha256
    printf 'tampered' >> artifact.tar.gz
    if (verify_release_checksum artifact.tar.gz) >/dev/null 2>&1; then echo 'FAIL: checksum mismatch accepted' >&2; exit 1; fi
)
printf 'PASS: semantic release JSON and ARM64 full/core resolution\n'
