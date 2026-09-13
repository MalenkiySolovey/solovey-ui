#!/bin/sh
set -eu

# Executes the bounded component/runtime-root test through each shipped Compose
# service definition. The adjacent override changes only the image, command,
# and isolated bind sources; the shipped service remains authoritative for the
# process identity, root policy, tmpfs, capabilities, security options,
# deployment profile, runtime root, network mode, and TUN device.

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
override="$repo_root/testsupport/docker/compose.component-runtime.override.yml"

: "${SOLOVEY_UI_COMPONENT_TEST_IMAGE:?set the bounded component-test image}"
: "${SOLOVEY_UI_COMPONENT_TEST_BINARY:?set the absolute component-test binary path}"

SOLOVEY_UI_IMAGE_DIGEST=${SOLOVEY_UI_IMAGE_DIGEST:-0000000000000000000000000000000000000000000000000000000000000000}
SOLOVEY_UI_PANEL_PORT=${SOLOVEY_UI_PANEL_PORT:-2053}
SOLOVEY_UI_PUBLIC_TCP_PORT=${SOLOVEY_UI_PUBLIC_TCP_PORT:-2443}
SOLOVEY_UI_PUBLIC_UDP_PORT=${SOLOVEY_UI_PUBLIC_UDP_PORT:-2443}
export SOLOVEY_UI_IMAGE_DIGEST SOLOVEY_UI_PANEL_PORT SOLOVEY_UI_PUBLIC_TCP_PORT SOLOVEY_UI_PUBLIC_UDP_PORT

test_root=$(mktemp -d)
case "$test_root" in
	/tmp/*) ;;
	*) echo "temporary test root is outside /tmp" >&2; exit 1 ;;
esac
project="sui-component-runtime-$$"
active_compose=
cleanup() {
	if [ -n "$active_compose" ]; then
		docker compose --project-name "$project" --file "$repo_root/$active_compose" --file "$override" down --remove-orphans >/dev/null 2>&1 || true
	fi
	rm -rf -- "$test_root"
}
trap cleanup EXIT HUP INT TERM

run_profile() {
	profile=$1
	compose_file=$2
	data_dir="$test_root/$profile/data"
	cert_dir="$test_root/$profile/cert"
	mkdir -p "$data_dir" "$cert_dir"
	chmod 0777 "$data_dir" "$cert_dir"
	export SOLOVEY_UI_COMPONENT_TEST_DATA="$data_dir"
	export SOLOVEY_UI_COMPONENT_TEST_CERT="$cert_dir"
	active_compose=$compose_file

	echo "shipped Compose authority: $profile <- $compose_file"
	docker compose \
		--project-name "$project" \
		--file "$repo_root/$compose_file" \
		--file "$override" \
		config --quiet
	docker compose \
		--project-name "$project" \
		--file "$repo_root/$compose_file" \
		--file "$override" \
		run --rm --no-deps solovey-ui
	docker compose \
		--project-name "$project" \
		--file "$repo_root/$compose_file" \
		--file "$override" \
		down --remove-orphans >/dev/null
	active_compose=
}

run_profile docker-host-unprivileged docker-compose.yml
run_profile docker-bridge-explicit deploy/docker/docker-compose.bridge.yml
run_profile docker-network-advanced deploy/docker/docker-compose.network-advanced.yml
