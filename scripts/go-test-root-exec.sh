#!/usr/bin/env bash
# Go's -exec adapter: build remains unprivileged, only the declared test binary
# runs as root. Copying also preserves the real root-owned executable contract.
set -euo pipefail
if [[ $(id -u) != 0 ]]; then
  exec sudo -n env "PATH=$PATH" "GOFLAGS=${GOFLAGS:-}" \
    "GOCOVERDIR=${GOCOVERDIR:-}" GOTOOLCHAIN=local \
    GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=safe.directory \
    "GIT_CONFIG_VALUE_0=$(git rev-parse --show-toplevel)" bash "$0" "$@"
fi
binary=$1
shift
root=$(mktemp -d /tmp/solovey-test-executor.XXXXXXXX)
trap 'rm -rf -- "$root"' EXIT
chmod 755 "$root"
install -m 755 "$binary" "$root/test.bin"
export GOCACHE=/root/.cache/solovey-test-go
# Nested Go fixtures must not create root-owned locks in the caller's cache.
export GOMODCACHE=/root/go/pkg/mod
"$root/test.bin" "$@"
