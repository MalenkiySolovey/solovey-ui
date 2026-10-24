#!/usr/bin/env bash
set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_ROOT=$(mktemp -d)
trap 'rm -rf "$TEST_ROOT"' EXIT
[[ $EUID != 0 ]] || { printf 'Run this fixture as an unprivileged user.\n' >&2; exit 1; }

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

fixture() {
    local name="$1" provider="$2" state="${3:-VERIFIED}" status="${4:-0}"
    local release="$TEST_ROOT/$name/app/releases/old"
    mkdir -p "$release"
    cat > "$release/solovey-ui" <<EOF
#!/bin/bash
case "\${1:-}" in
  core-info) printf '%s\\n' '{"schema":"solovey.core-provider-identity/v1","state":"$state","id":"$provider"}'; exit $status ;;
  -v) printf 'Solovey UI Panel\\t 2026.4.1\\nSing-Box\\t v1.14.2\\n' ;;
  *) exit 2 ;;
esac
EOF
    chmod 0755 "$release/solovey-ui"
    printf 'app=solovey-ui\nversion=2026.4.1\nprofile=core\nsing_box=1.14.2\ncore_provider=%s\n' "$provider" > "$release/BUILD_INFO.txt"
    ln -s old "$TEST_ROOT/$name/app/releases/current"
}

select_artifact() {
    local name="$1" arch="$2"
    shift 2
    env SOLOVEY_UI_ALLOW_NON_ROOT=1 SOLOVEY_UI_CORE_PROVIDER= \
        SOLOVEY_UI_INSTALL_DIR="$TEST_ROOT/$name/app" \
        SOLOVEY_UI_ENV_DIR="$TEST_ROOT/$name/etc" \
        SOLOVEY_UI_HARDENED_DATA_ROOT="$TEST_ROOT/$name/data" \
        SOLOVEY_UI_LEGACY_DIR="$TEST_ROOT/$name/legacy" \
        SOLOVEY_UI_LEGACY_DB="$TEST_ROOT/$name/legacy/db/s-ui.db" \
        bash -c 'source "$1/install.sh"; arch=$2; shift 2; parse_args "$@"; resolve_core_provider; release_artifact_name "$arch"' fixture "$ROOT" "$arch" "$@"
}

accept() {
    local expected="$1" actual
    shift
    actual=$(select_artifact "$@") || fail "selection rejected: $*"
    [[ "$actual" == "$expected" ]] || fail "expected $expected, got $actual"
}

reject() {
    if select_artifact "$@" > "$TEST_ROOT/rejected.log" 2>&1; then
        fail "unsafe provider selection accepted: $*"
    fi
}

# Both component footprints retain their existing Official artifact identity.
accept solovey-ui-linux-amd64.tar.gz fresh amd64
accept solovey-ui-core-linux-arm64.tar.gz fresh arm64 --profile core
accept solovey-ui-shtorm-extended-linux-amd64.tar.gz fresh amd64 --core-provider shtorm-extended
accept solovey-ui-shtorm-extended-core-linux-amd64.tar.gz fresh amd64 --profile core --core-provider shtorm-extended
reject fresh arm64 --core-provider shtorm-extended
reject fresh amd64 --core-provider unknown
reject fresh amd64 --core-provider shtorm-extended --core-provider sing-box

fixture official sing-box
fixture extended shtorm-extended
accept solovey-ui-core-linux-amd64.tar.gz official amd64 --profile core
accept solovey-ui-shtorm-extended-core-linux-amd64.tar.gz extended amd64 --profile core
accept solovey-ui-shtorm-extended-linux-amd64.tar.gz extended amd64 --core-provider shtorm-extended
# Crossing providers is owned by the authenticated maintenance lifecycle.
reject official amd64 --core-provider shtorm-extended
reject extended amd64 --core-provider sing-box

fixture unknown shtorm-extended UNKNOWN
reject unknown amd64
fixture failed shtorm-extended VERIFIED 1
reject failed amd64
fixture foreign foreign-core
reject foreign amd64

# The supported stable binary predates core-info. Its bounded legacy output
# classifies only the existing unflavored Host installation, never source trust.
fixture legacy sing-box
sed -i '/^core_provider=/d' "$TEST_ROOT/legacy/app/releases/old/BUILD_INFO.txt"
cat > "$TEST_ROOT/legacy/app/releases/old/solovey-ui" <<'SH'
#!/bin/bash
if [[ ${1:-} == -v ]]; then
    printf 'Solovey UI Panel\t 2026.4.1\nSing-Box\t v1.14.2\n'
else
    printf 'Invalid subcommands\n' >&2
    exit 1
fi
SH
chmod 0755 "$TEST_ROOT/legacy/app/releases/old/solovey-ui"
accept solovey-ui-core-linux-amd64.tar.gz legacy amd64 --profile core
reject legacy amd64 --core-provider shtorm-extended

# A modern UNKNOWN observation never falls back to the legacy default, even
# when a metadata field was lost and -v happens to resemble the older command.
fixture lost-metadata shtorm-extended UNKNOWN
sed -i '/^core_provider=/d' "$TEST_ROOT/lost-metadata/app/releases/old/BUILD_INFO.txt"
reject lost-metadata amd64

fixture duplicate sing-box
sed -i 's/"id":"sing-box"/"id":"sing-box","id":"shtorm-extended"/' "$TEST_ROOT/duplicate/app/releases/old/solovey-ui"
reject duplicate amd64

fixture conflicting sing-box
sed -i 's/^core_provider=sing-box$/core_provider=shtorm-extended/' "$TEST_ROOT/conflicting/app/releases/old/BUILD_INFO.txt"
reject conflicting amd64
mkdir -p "$TEST_ROOT/broken/app/releases"
ln -s missing "$TEST_ROOT/broken/app/releases/current"
reject broken amd64
mkdir -p "$TEST_ROOT/directory/app/releases/current"
reject directory amd64

# A database without a live provider is existing state, not a fresh opt-in.
mkdir -p "$TEST_ROOT/database-only/data/db"
printf 'preserved state\n' > "$TEST_ROOT/database-only/data/db/solovey-ui.db"
accept solovey-ui-linux-amd64.tar.gz database-only amd64
reject database-only amd64 --core-provider shtorm-extended
[[ $(cat "$TEST_ROOT/database-only/data/db/solovey-ui.db") == 'preserved state' ]]
mkdir -p "$TEST_ROOT/retained-env/etc/db"
printf 'foreign legacy state\n' > "$TEST_ROOT/retained-env/etc/db/other.db"
reject retained-env amd64 --core-provider shtorm-extended

check_payload() {
    local provider="$1" fixture_name="$2"
    env SOLOVEY_UI_ALLOW_NON_ROOT=1 bash -c 'source "$1/install.sh"; CORE_PROVIDER=$2; validate_payload_core_provider "$3"' \
        fixture "$ROOT" "$provider" "$TEST_ROOT/$fixture_name/app/releases/old"
}
check_payload sing-box official
check_payload shtorm-extended extended
check_payload sing-box legacy
for pair in 'sing-box extended' 'shtorm-extended official' 'sing-box unknown' 'sing-box conflicting'; do
    read -r selected fixture_name <<< "$pair"
    if check_payload "$selected" "$fixture_name" > "$TEST_ROOT/rejected.log" 2>&1; then
        fail "payload identity mismatch accepted: $pair"
    fi
done

printf 'PASS: installer provider selection, retention, legacy compatibility and maintenance boundary\n'
