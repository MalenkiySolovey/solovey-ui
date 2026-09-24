#!/usr/bin/env bash

set -Eeuo pipefail

# This is the single owner of Linux target tags, linker flags and Go package
# entrypoints. CI and the OpenWrt stage producer supply only target coordinates,
# a compiler environment and an output directory.

readonly BASE_TAGS='with_quic,with_grpc,with_utls,with_acme,with_gvisor,badlinkname,tfogo_checklinkname0,with_tailscale'
readonly HELPER_LDFLAGS="-buildid= -w -s -linkmode external -extldflags '-static'"

mode=''
out=''
platform=''
trust_roots_file=''
profile='full'
naive='false'

usage() {
	cat <<'EOF'
Usage: scripts/build-linux-target.sh --mode <release|openwrt> --out <directory> --platform <artifact-platform> --trust-roots-file <base64-file> [--profile <full|core>] [--naive]

The caller must set GOOS, GOARCH, CGO_ENABLED, CC and CXX. OpenWrt mode adds
the with_musl build tag and emits all nine package executables. Release mode
emits the five generic full-profile release executables.
EOF
}

fail() {
	echo "[build-linux-target] ERROR: $*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--mode) mode=${2:?--mode requires a value}; shift 2 ;;
		--out) out=${2:?--out requires a value}; shift 2 ;;
		--platform) platform=${2:?--platform requires a value}; shift 2 ;;
		--trust-roots-file) trust_roots_file=${2:?--trust-roots-file requires a value}; shift 2 ;;
		--profile) profile=${2:?--profile requires a value}; shift 2 ;;
		--naive) naive='true'; shift ;;
		--help|-h) usage; exit 0 ;;
		*) usage; fail "unknown argument: $1" ;;
	esac
done

[[ "$mode" == 'release' || "$mode" == 'openwrt' ]] || fail '--mode must be release or openwrt'
[[ "$profile" == 'full' || "$profile" == 'core' ]] || fail '--profile must be full or core'
[[ "$mode" != 'openwrt' || "$profile" == 'full' ]] || fail 'OpenWrt stage supports only the full profile'
[[ "$mode" != 'openwrt' || "${SOLOVEY_HERMETIC_TARGET_ENV:-}" == '1' ]] || fail 'OpenWrt targets must run through the canonical hermetic stage producer'
[[ -n "$out" && -n "$platform" && -f "$trust_roots_file" ]] || fail 'output, platform and trust-roots file are required'
[[ "${GOOS:-}" == 'linux' && -n "${GOARCH:-}" && "${CGO_ENABLED:-}" == '1' ]] || fail 'GOOS=linux, GOARCH and CGO_ENABLED=1 are required'
[[ -n "${CC:-}" && -n "${CXX:-}" ]] || fail 'CC and CXX are required'
if [[ "$mode" == 'release' ]]; then
	node "$(dirname "${BASH_SOURCE[0]}")/release-target.mjs" "$platform" "$naive" || fail 'release target identity is invalid'
fi
go_program=$(command -v go) || fail 'go is unavailable'
go_program=$(readlink -f "$go_program")
if [[ "$mode" == 'openwrt' ]]; then
	[[ "${SOLOVEY_GO_AUTHORITY_VERIFIED:-}" == '1' ]] || fail 'OpenWrt Go authority was not verified at the final use boundary'
	[[ -n "${GOROOT:-}" && "$go_program" == "$(readlink -f "$GOROOT/bin/go")" ]] || fail 'OpenWrt Go executable differs from the private GOROOT authority'
	[[ -n "${GOMODCACHE:-}" && -n "${GOCACHE:-}" && -n "${GOPATH:-}" ]] || fail 'OpenWrt private Go cache roots are unavailable'
fi
[[ ! -e "$out" ]] || fail "output already exists: $out"

trust_roots=$(tr -d '\r\n' < "$trust_roots_file")
[[ -n "$trust_roots" ]] || fail 'release trust roots are empty'
tags=$BASE_TAGS
if [[ "$mode" == 'openwrt' ]]; then
	tags="${tags},with_naive_outbound,with_musl"
elif [[ "$naive" == 'true' ]]; then
	tags="${tags},with_naive_outbound,with_musl"
fi
if [[ "$profile" == 'core' ]]; then
	tags="${tags},minimal"
fi
panel_ldflags="-buildid= -w -s -checklinkname=0 -linkmode external -extldflags '-static' -X github.com/MalenkiySolovey/solovey-ui/config/update.ArtifactPlatform=${platform} -X github.com/MalenkiySolovey/solovey-ui/config/update.ReleaseTrustRootsBase64=${trust_roots}"

mkdir -p "$out"
panel_name='solovey-ui'
[[ "$profile" == 'core' ]] && panel_name='solovey-ui-core'
"$go_program" build -trimpath -buildvcs=false -ldflags="$panel_ldflags" -tags "$tags" -o "$out/$panel_name" main.go

if [[ "$profile" == 'core' ]]; then
	chmod 0755 "$out/$panel_name"
	echo "[build-linux-target] built $mode $profile target in $out"
	exit 0
fi

"$go_program" build -trimpath -buildvcs=false -ldflags="$HELPER_LDFLAGS" -o "$out/solovey-privileged-broker" ./cmd/solovey-privileged-broker
"$go_program" build -trimpath -buildvcs=false -ldflags="$HELPER_LDFLAGS" -o "$out/solovey-ssh-proof" ./cmd/solovey-ssh-proof

if [[ "$mode" == 'openwrt' ]]; then
	"$go_program" build -trimpath -buildvcs=false -ldflags="$HELPER_LDFLAGS" -o "$out/solovey-evidence" ./cmd/solovey-evidence
	"$go_program" build -trimpath -buildvcs=false -ldflags="$HELPER_LDFLAGS" -o "$out/solovey-broker-readiness" ./cmd/solovey-broker-readiness
	"$go_program" build -trimpath -buildvcs=false -ldflags="$HELPER_LDFLAGS" -o "$out/solovey-openwrt-preservation" ./cmd/solovey-openwrt-preservation
	"$go_program" build -trimpath -buildvcs=false -ldflags="$HELPER_LDFLAGS" -o "$out/solovey-openwrt-durability" ./cmd/solovey-openwrt-durability
	"$go_program" build -trimpath -buildvcs=false -ldflags="$HELPER_LDFLAGS" -o "$out/solovey-openwrt-lifecycle" ./cmd/solovey-openwrt-lifecycle
	"$go_program" build -trimpath -buildvcs=false -ldflags="$HELPER_LDFLAGS" -o "$out/solovey-openwrt-owner-manifest" ./components/server-protection/cmd/solovey-openwrt-owner-manifest
	"$go_program" build -trimpath -buildvcs=false -ldflags="$HELPER_LDFLAGS" -o "$out/solovey-openwrt-broker-manifest" ./cmd/solovey-openwrt-broker-manifest
else
	"$go_program" build -trimpath -buildvcs=false -ldflags="$HELPER_LDFLAGS" -o "$out/solovey-broker-manifest" ./cmd/solovey-broker-manifest
	"$go_program" build -trimpath -buildvcs=false -ldflags="$HELPER_LDFLAGS" -o "$out/solovey-owner-manifest" ./components/server-protection/cmd/solovey-owner-manifest
fi

chmod 0755 "$out"/*
echo "[build-linux-target] built $mode $profile target in $out"
