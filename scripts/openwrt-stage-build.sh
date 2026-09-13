#!/usr/bin/env bash

set -Eeuo pipefail

# Canonical source-owned producer for the complete OpenWrt package stage.
# It snapshots explicit target inputs, regenerates all deterministic outputs,
# builds the frontend before Go embedding, compiles the nine target programs,
# and closes the result with a source-bound payload manifest.

readonly GO_VERSION='go1.26.6'
readonly GO_TOOLCHAIN_FILENAME='go1.26.6.linux-amd64.tar.gz'
readonly GO_TOOLCHAIN_SHA256='708effb774be8237570d0add163225abbdfaf4fca28b2611df167beba4feef89'
readonly CRONET_TOOLCHAIN_COMMIT='e7f6f6f5b7ce226f686f6cb5d068a63da6657ccd'
readonly BUILD_TAGS='with_quic,with_grpc,with_utls,with_acme,with_gvisor,badlinkname,tfogo_checklinkname0,with_tailscale,with_naive_outbound,with_musl'
readonly HELPER_LDFLAGS_CONTRACT="-buildid= -w -s -linkmode external -extldflags '-static'"

source_dir=''
out_dir=''
trust_roots_file=''
go_toolchain_archive=''
target_profile='x86-64'
temporary_root=''

source "$(dirname "${BASH_SOURCE[0]}")/openwrt-target-profile.sh"

usage() {
	cat <<'EOF'
Usage: scripts/openwrt-stage-build.sh --source <current-source> --out <new-stage-root> --trust-roots-file <public-base64-file> --go-toolchain-archive <go1.26.6.linux-amd64.tar.gz> [--target-profile <x86-64|rockchip-armv8>]

Required environment: the project-pinned Go and Node toolchains plus
GOOS=linux, the profile's GOARCH, CGO_ENABLED=1 and the OpenWrt-musl CC/CXX compiler
commands. The output must not already exist.
EOF
}

fail() {
	echo "[openwrt-stage-build] ERROR: $*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--source) source_dir=${2:?--source requires a value}; shift 2 ;;
		--out) out_dir=${2:?--out requires a value}; shift 2 ;;
		--trust-roots-file) trust_roots_file=${2:?--trust-roots-file requires a value}; shift 2 ;;
		--go-toolchain-archive) go_toolchain_archive=${2:?--go-toolchain-archive requires a value}; shift 2 ;;
		--target-profile) target_profile=${2:?--target-profile requires a value}; shift 2 ;;
		--help|-h) usage; exit 0 ;;
		*) usage; fail "unknown argument: $1" ;;
	esac
done

openwrt_target_profile_load "$target_profile" || exit $?
readonly PANEL_LDFLAGS_CONTRACT="-buildid= -w -s -checklinkname=0 -linkmode external -extldflags '-static' -X config/update.ArtifactPlatform=$OPENWRT_PLATFORM -X config/update.ReleaseTrustRootsBase64=<bound-public-roots>"

[[ -n "$source_dir" && -n "$out_dir" && -f "$trust_roots_file" && -f "$go_toolchain_archive" ]] || { usage; exit 2; }
for command in git node npm sha256sum tar; do
	command -v "$command" >/dev/null || fail "required build command is unavailable: $command"
done
[[ "${GOOS:-}" == 'linux' && "${GOARCH:-}" == "$OPENWRT_GOARCH" && "${CGO_ENABLED:-}" == '1' ]] || fail "GOOS=linux GOARCH=$OPENWRT_GOARCH CGO_ENABLED=1 are required"
[[ -n "${CC:-}" && -n "${CXX:-}" ]] || fail 'CC and CXX are required'
[[ "${CGO_LDFLAGS:-}" == '-fuse-ld=lld' ]] || fail 'CGO_LDFLAGS=-fuse-ld=lld is required'

# Known release-unsafe selectors and toolchain overrides fail closed. The
# target-generating commands below run through env -i, so unrelated ambient
# variables (including HOME and user npm/Go configuration) are not inherited.
while IFS='=' read -r variable _; do
	case "$variable" in
		SUI_E2E|SUI_E2E_*|VITE_*|SOLOVEY_UI_PROFILE|SOLOVEY_UI_COMPONENT_IDS)
			fail "forbidden target selector is set: $variable"
			;;
	esac
done < <(env)

for variable in GOROOT GOMODCACHE GOPATH GOCACHE GOFLAGS GOEXPERIMENT GOENV GOWORK GOTOOLCHAIN GO111MODULE GOAMD64 \
	GODEBUG GOFIPS140 GOTMPDIR GOPROXY GOSUMDB GONOSUMDB GOPRIVATE GONOPROXY GOVCS GOINSECURE GOAUTH GOTELEMETRY \
	CGO_CFLAGS CGO_CPPFLAGS CGO_CXXFLAGS CGO_FFLAGS \
	PKG_CONFIG NODE_OPTIONS BROWSERSLIST BROWSERSLIST_ENV BROWSERSLIST_CONFIG; do
	if [[ -v "$variable" ]]; then
		fail "forbidden target environment override is set: $variable"
	fi
done

read -r -a cc_command <<< "$CC"
read -r -a cxx_command <<< "$CXX"
[[ ${#cc_command[@]} -eq 3 ]] || fail 'CC must be the canonical compiler plus exactly target and sysroot arguments'
[[ ${#cxx_command[@]} -eq 3 ]] || fail 'CXX must be the canonical compiler plus exactly target and sysroot arguments'
[[ "${cc_command[1]}" == "--target=$OPENWRT_CC_TARGET" ]] || fail 'CC target argument is not canonical for the selected OpenWrt profile'
[[ "${cxx_command[1]}" == "--target=$OPENWRT_CC_TARGET" ]] || fail 'CXX target argument is not canonical for the selected OpenWrt profile'
[[ "${cc_command[2]}" == --sysroot=* && "${cxx_command[2]}" == --sysroot=* ]] || fail 'CC and CXX must name one canonical sysroot argument'
[[ "${cc_command[2]}" == "${cxx_command[2]}" ]] || fail 'CC and CXX sysroots differ'
umask 022

source_dir=$(readlink -f "$source_dir")
trust_roots_file=$(readlink -f "$trust_roots_file")
go_toolchain_archive=$(readlink -f "$go_toolchain_archive")
[[ -d "$source_dir" ]] || fail 'source root is unavailable'
[[ -f "$source_dir/config/identity/version" ]] || fail 'source root is not Solovey UI'
[[ ! -e "$out_dir" ]] || fail "stage output already exists: $out_dir"

temporary_root=$(mktemp -d)
[[ "$temporary_root" == /tmp/tmp.* ]] || fail 'private temporary root is outside the canonical /tmp boundary'
cleanup_temporary_root() {
	chmod -R u+w "$temporary_root" 2>/dev/null || true
	rm -rf -- "$temporary_root"
}
trap cleanup_temporary_root EXIT
snapshot="$temporary_root/source"
binary_dir="$temporary_root/binaries"
build_facts="$temporary_root/BUILD_FACTS.json"

mkdir -m 0700 "$temporary_root/home" "$temporary_root/tmp" "$temporary_root/xdg-config" \
	"$temporary_root/xdg-cache" "$temporary_root/npm-cache" "$temporary_root/go-cache" \
	"$temporary_root/gopath" "$temporary_root/go-mod-cache" "$temporary_root/go-toolchain"
: > "$temporary_root/npm-userconfig"
: > "$temporary_root/npm-globalconfig"

node_program=$(readlink -f "$(command -v node)")
npm_program=$(readlink -f "$(command -v npm)")
cc_program=$(readlink -f "${cc_command[0]}")
cxx_program=$(readlink -f "${cxx_command[0]}")
[[ -x "$node_program" && -f "$npm_program" ]] || fail 'canonical Node/npm executables are unavailable'
[[ -x "$cc_program" && -x "$cxx_program" ]] || fail 'canonical CC/CXX executables are unavailable'
cc_command[0]=$cc_program
cxx_command[0]=$cxx_program
CC="${cc_command[*]}"
CXX="${cxx_command[*]}"

canonical_path="$(dirname "$node_program"):$(dirname "$npm_program"):/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
common_environment=(
	"PATH=$canonical_path"
	"HOME=$temporary_root/home"
	"XDG_CONFIG_HOME=$temporary_root/xdg-config"
	"XDG_CACHE_HOME=$temporary_root/xdg-cache"
	"TMPDIR=$temporary_root/tmp"
	'SOURCE_DATE_EPOCH=0'
	'TZ=UTC'
	'LC_ALL=C'
	'LANG=C'
)

private_go_archive="$temporary_root/go-toolchain/$GO_TOOLCHAIN_FILENAME"
env -i "${common_environment[@]}" "$node_program" "$source_dir/scripts/openwrt-go-authority.mjs" pin-archive \
	--source "$go_toolchain_archive" --out "$private_go_archive" --sha256 "$GO_TOOLCHAIN_SHA256"
tar -tzf "$private_go_archive" | env -i "${common_environment[@]}" "$node_program" \
	"$source_dir/scripts/openwrt-go-authority.mjs" validate-list --expected-top go
tar -xzf "$private_go_archive" --directory "$temporary_root/go-toolchain" --no-same-owner
go_root="$temporary_root/go-toolchain/go"
go_program="$go_root/bin/go"
[[ -x "$go_program" ]] || fail 'private Go toolchain executable is unavailable'
go_toolchain_proof="$temporary_root/GO_TOOLCHAIN_AUTHORITY.json"
env -i "${common_environment[@]}" "$node_program" "$source_dir/scripts/openwrt-go-authority.mjs" create-toolchain \
	--root "$go_root" --archive-filename "$GO_TOOLCHAIN_FILENAME" --archive-sha256 "$GO_TOOLCHAIN_SHA256" \
	--version "$GO_VERSION" --goos linux --goarch "$OPENWRT_GOARCH" --out "$go_toolchain_proof"
go_version=$(env -i "${common_environment[@]}" GOROOT="$go_root" GOENV=off GOTOOLCHAIN=local "$go_program" version | sed -E 's#go version (go[^ ]+) .*#\1#')
[[ "$go_version" == "$GO_VERSION" ]] || fail 'private Go toolchain version mismatch'
canonical_path="$(dirname "$go_program"):$canonical_path"
common_environment[0]="PATH=$canonical_path"
frontend_environment=(
	"${common_environment[@]}"
	'CI=true'
	'NODE_OPTIONS='
	'SOLOVEY_UI_PROFILE=full'
	"NPM_CONFIG_CACHE=$temporary_root/npm-cache"
	"NPM_CONFIG_USERCONFIG=$temporary_root/npm-userconfig"
	"NPM_CONFIG_GLOBALCONFIG=$temporary_root/npm-globalconfig"
	'NPM_CONFIG_IGNORE_SCRIPTS=false'
	'NPM_CONFIG_PRODUCTION=false'
	'NPM_CONFIG_AUDIT=false'
	'NPM_CONFIG_FUND=false'
	'NPM_CONFIG_UPDATE_NOTIFIER=false'
)
go_environment=(
	"${common_environment[@]}"
	"GOROOT=$go_root"
	"GOMODCACHE=$temporary_root/go-mod-cache"
	"GOCACHE=$temporary_root/go-cache"
	"GOPATH=$temporary_root/gopath"
	'GOOS=linux'
	"GOARCH=$OPENWRT_GOARCH"
	'CGO_ENABLED=1'
	"CC=$CC"
	"CXX=$CXX"
	'CGO_CFLAGS=-O2 -g'
	'CGO_CPPFLAGS='
	'CGO_CXXFLAGS=-O2 -g'
	'CGO_FFLAGS=-O2 -g'
	'CGO_LDFLAGS=-fuse-ld=lld'
	'GOFLAGS=-mod=readonly'
	'GOENV=off'
	'GOWORK=off'
	'GOTOOLCHAIN=local'
	'GO111MODULE=on'
	'GOTELEMETRY=off'
	'GOAUTH=off'
	'GOINSECURE='
	'GOPRIVATE='
	'GONOPROXY='
	'GONOSUMDB='
	'GOPROXY=https://proxy.golang.org'
	'GOSUMDB=sum.golang.org'
	'GOVCS=*:off'
	"GOTMPDIR=$temporary_root/tmp"
	'PKG_CONFIG=/usr/bin/false'
	'SOLOVEY_HERMETIC_TARGET_ENV=1'
)
if [[ -n "$OPENWRT_GOARCH_VARIANT" ]]; then
	go_environment+=("GOAMD64=$OPENWRT_GOARCH_VARIANT")
fi

run_node() {
	env -i "${frontend_environment[@]}" "$node_program" "$@"
}

run_npm() {
	env -i "${frontend_environment[@]}" "$npm_program" "$@"
}

run_go() {
	env -i "${go_environment[@]}" "$go_program" "$@"
}

materialize_go_dependencies() {
	local attempt
	for attempt in 1 2 3 4 5; do
		if run_go list -deps -json -tags "$BUILD_TAGS" . \
			./cmd/solovey-privileged-broker ./cmd/solovey-ssh-proof ./cmd/solovey-evidence ./cmd/solovey-broker-readiness \
			./cmd/solovey-openwrt-preservation ./components/server-protection/cmd/solovey-openwrt-owner-manifest \
			./cmd/solovey-openwrt-broker-manifest ./cmd/solovey-openwrt-durability ./cmd/solovey-openwrt-lifecycle >/dev/null; then
			return 0
		fi
		[[ "$attempt" -lt 5 ]] || fail 'authenticated Go target dependency materialization failed after five attempts'
		echo "[openwrt-stage-build] retrying authenticated Go dependency materialization ($attempt/5)" >&2
	done
}

# Existing checked-in generated Go is a reviewable contract and must already
# agree with the current component catalog. The isolated source snapshot is
# then regenerated independently before compilation.
run_node "$source_dir/scripts/generate-component-imports.mjs" --profile full --check
run_node "$source_dir/scripts/openwrt-persistence-authority.mjs" --check
run_node "$source_dir/scripts/openwrt-package-source-fingerprint.mjs" materialize --source-root "$source_dir" --out "$snapshot"

(
	cd "$snapshot"
	run_node scripts/generate-component-imports.mjs --profile full
	cd frontend
	run_npm ci
	run_npm run build
)
run_node "$snapshot/scripts/check-frontend-profile.mjs" --dist "$snapshot/frontend/dist" \
	--components-dir "$snapshot/components" --profile full
run_node "$snapshot/scripts/extract-component-frontend.mjs" \
	--dist "$snapshot/frontend/dist" --components-dir "$snapshot/components" \
	--out-dir "$snapshot/.release/components" --prune-dist
run_node "$snapshot/scripts/write-component-installed-metadata.mjs" \
	--components-dir "$snapshot/.release/components" \
	--out "$snapshot/.release/components/installed.json" --profile full --binary full
run_node "$snapshot/scripts/frontend-runtime-closure.mjs" \
	--dist "$snapshot/frontend/dist" --components-dir "$snapshot/.release/components" \
	--out "$snapshot/.release/components/runtime-closure.json"
mkdir -p "$snapshot/web/html"
cp -a "$snapshot/frontend/dist/." "$snapshot/web/html/"

go_dependency_proof="$temporary_root/GO_DEPENDENCY_AUTHORITY.json"
(
	cd "$snapshot"
	materialize_go_dependencies
)
(
	cd "$snapshot"
	run_go mod verify
)
(
	cd "$snapshot"
	run_go list -deps -json -tags "$BUILD_TAGS" . \
		./cmd/solovey-privileged-broker ./cmd/solovey-ssh-proof ./cmd/solovey-evidence ./cmd/solovey-broker-readiness \
		./cmd/solovey-openwrt-preservation ./components/server-protection/cmd/solovey-openwrt-owner-manifest \
		./cmd/solovey-openwrt-broker-manifest ./cmd/solovey-openwrt-durability ./cmd/solovey-openwrt-lifecycle
) | run_node "$source_dir/scripts/openwrt-go-authority.mjs" create-dependencies \
	--graph package --go-mod "$snapshot/go.mod" --go-sum "$snapshot/go.sum" --out "$go_dependency_proof"

go_toolchain_identity=$(run_node -e "const f=require(process.argv[1]); process.stdout.write(f.toolchainIdentity)" "$go_toolchain_proof")
go_toolchain_tree_digest=$(run_node -e "const f=require(process.argv[1]); process.stdout.write(f.tree.treeDigest)" "$go_toolchain_proof")
go_toolchain_entry_count=$(run_node -e "const f=require(process.argv[1]); process.stdout.write(String(f.tree.entryCount))" "$go_toolchain_proof")
go_dependency_identity=$(run_node -e "const f=require(process.argv[1]); process.stdout.write(f.dependencyIdentity)" "$go_dependency_proof")
go_module_graph_digest=$(run_node -e "const f=require(process.argv[1]); process.stdout.write(f.modules.moduleGraphDigest)" "$go_dependency_proof")
go_module_count=$(run_node -e "const f=require(process.argv[1]); process.stdout.write(String(f.modules.count))" "$go_dependency_proof")
go_mod_sha256=$(run_node -e "const f=require(process.argv[1]); process.stdout.write(f.source.goMod.sha256)" "$go_dependency_proof")
go_sum_sha256=$(run_node -e "const f=require(process.argv[1]); process.stdout.write(f.source.goSum.sha256)" "$go_dependency_proof")

node_version=$(run_node --version)
npm_version=$(run_npm --version)
compiler_identity=$(env -i "${common_environment[@]}" "$cc_program" --version | sed -n '1p' | sed -E 's#[[:space:]]+# #g; s#^ ##; s# $##')
cxx_identity=$(env -i "${common_environment[@]}" "$cxx_program" --version | sed -n '1p' | sed -E 's#[[:space:]]+# #g; s#^ ##; s# $##')
sysroot=${cc_command[2]#--sysroot=}
[[ -d "$sysroot" ]] || fail 'compiler sysroot is unavailable'
resource_dir=$(env -i "${common_environment[@]}" "$cc_program" -print-resource-dir)
[[ -d "$resource_dir" ]] || fail 'compiler resource directory is unavailable'
lld_program=$(dirname "$cc_program")/ld.lld
[[ -f "$lld_program" ]] || fail 'toolchain linker is unavailable'
compiler_repository=$(git -C "$(dirname "$cc_program")" rev-parse --show-toplevel)
cronet_root=$(git -C "$compiler_repository" rev-parse --show-superproject-working-tree)
[[ -n "$cronet_root" ]] || cronet_root=$compiler_repository
cronet_commit=$(git -C "$cronet_root" rev-parse HEAD)
[[ "$cronet_commit" == "$CRONET_TOOLCHAIN_COMMIT" ]] || fail 'cronet toolchain source revision mismatch'

hash_tree() {
	(
		cd "$1"
		{
			find . -type f -print0 | LC_ALL=C sort -z | xargs -0 -r sha256sum
			find . -type l -printf 'L %p %l\n' | LC_ALL=C sort
		} | sha256sum | awk '{print $1}'
	)
}

go_sha256=$(sha256sum "$go_program" | awk '{print $1}')
node_sha256=$(sha256sum "$node_program" | awk '{print $1}')
npm_sha256=$(sha256sum "$npm_program" | awk '{print $1}')
cc_sha256=$(sha256sum "$cc_program" | awk '{print $1}')
cxx_sha256=$(sha256sum "$cxx_program" | awk '{print $1}')
lld_sha256=$(sha256sum "$lld_program" | awk '{print $1}')
sysroot_sha256=$(hash_tree "$sysroot")
resource_dir_sha256=$(hash_tree "$resource_dir")
trust_roots_sha256=$(sha256sum "$trust_roots_file" | awk '{print $1}')
commit=$(git -C "$source_dir" rev-parse HEAD)
source_date=$(git -C "$source_dir" show -s --format=%cs HEAD)
[[ "$source_date" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || fail 'source revision date is unavailable'

env -i "${frontend_environment[@]}" \
	BF_GO_VERSION="$go_version" BF_NODE_VERSION="$node_version" BF_NPM_VERSION="$npm_version" \
	BF_GO_SHA256="$go_sha256" BF_NODE_SHA256="$node_sha256" BF_NPM_SHA256="$npm_sha256" \
	BF_GO_TOOLCHAIN_ARCHIVE_FILENAME="$GO_TOOLCHAIN_FILENAME" BF_GO_TOOLCHAIN_ARCHIVE_SHA256="$GO_TOOLCHAIN_SHA256" \
	BF_GO_TOOLCHAIN_IDENTITY="$go_toolchain_identity" BF_GO_TOOLCHAIN_TREE_DIGEST="$go_toolchain_tree_digest" \
	BF_GO_TOOLCHAIN_ENTRY_COUNT="$go_toolchain_entry_count" BF_GO_DEPENDENCY_IDENTITY="$go_dependency_identity" \
	BF_GO_MODULE_GRAPH_DIGEST="$go_module_graph_digest" BF_GO_MODULE_COUNT="$go_module_count" \
	BF_GO_MOD_SHA256="$go_mod_sha256" BF_GO_SUM_SHA256="$go_sum_sha256" \
BF_COMPILER_IDENTITY="$compiler_identity" BF_CXX_IDENTITY="$cxx_identity" \
BF_CC_SHA256="$cc_sha256" BF_CXX_SHA256="$cxx_sha256" BF_LLD_SHA256="$lld_sha256" \
BF_CC_TARGET_ARGUMENT="${cc_command[1]}" BF_CXX_TARGET_ARGUMENT="${cxx_command[1]}" \
BF_SYSROOT_SHA256="$sysroot_sha256" BF_RESOURCE_DIR_SHA256="$resource_dir_sha256" \
BF_CRONET_COMMIT="$cronet_commit" \
BF_TRUST_ROOTS_SHA256="$trust_roots_sha256" BF_BUILD_TAGS="$BUILD_TAGS" \
BF_PANEL_LDFLAGS="$PANEL_LDFLAGS_CONTRACT" BF_HELPER_LDFLAGS="$HELPER_LDFLAGS_CONTRACT" \
BF_SDK_FILENAME="$OPENWRT_SDK_FILENAME" BF_SDK_SHA256="$OPENWRT_SDK_SHA256" \
BF_OPENWRT_RELEASE="$OPENWRT_RELEASE" BF_SOURCE_TAG="$OPENWRT_SOURCE_TAG" \
BF_OPENWRT_REVISION="$OPENWRT_REVISION" BF_PROFILE_KEY="$OPENWRT_PROFILE_KEY" \
BF_SOURCE_COMMIT="$OPENWRT_SOURCE_COMMIT" BF_TARGET="$OPENWRT_TARGET" \
BF_SUBTARGET="$OPENWRT_SUBTARGET" BF_PACKAGE_ARCH="$OPENWRT_PACKAGE_ARCH" \
BF_GOARCH="$OPENWRT_GOARCH" BF_GOARCH_VARIANT="$OPENWRT_GOARCH_VARIANT" \
BF_PLATFORM="$OPENWRT_PLATFORM" BF_CC_TARGET="$OPENWRT_CC_TARGET" \
BF_SOURCE_REVISION="$commit" BF_SOURCE_DATE="$source_date" BF_OUT="$build_facts" \
"$node_program" --input-type=module - <<'NODE'
import fs from 'node:fs'
const e = process.env
const facts = {
  revision: { commit: e.BF_SOURCE_REVISION, sourceDate: e.BF_SOURCE_DATE },
  target: {
    profile: e.BF_PROFILE_KEY,
    release: e.BF_OPENWRT_RELEASE,
    sourceTag: e.BF_SOURCE_TAG,
    revision: e.BF_OPENWRT_REVISION,
    sourceCommit: e.BF_SOURCE_COMMIT,
    target: e.BF_TARGET,
    subtarget: e.BF_SUBTARGET,
    packageArchitecture: e.BF_PACKAGE_ARCH,
    sdkFilename: e.BF_SDK_FILENAME,
    sdkSha256: e.BF_SDK_SHA256,
    goarch: e.BF_GOARCH,
    platform: e.BF_PLATFORM,
    compilerTarget: e.BF_CC_TARGET,
  },
  go: {
    version: e.BF_GO_VERSION,
    executableSha256: e.BF_GO_SHA256,
    toolchain: {
      archiveFilename: e.BF_GO_TOOLCHAIN_ARCHIVE_FILENAME,
      archiveSha256: e.BF_GO_TOOLCHAIN_ARCHIVE_SHA256,
      treeDigest: e.BF_GO_TOOLCHAIN_TREE_DIGEST,
      entryCount: Number(e.BF_GO_TOOLCHAIN_ENTRY_COUNT),
      identity: e.BF_GO_TOOLCHAIN_IDENTITY,
      authentication: 'official-go-download-sha256-private-copy',
    },
    dependencies: {
      goModSha256: e.BF_GO_MOD_SHA256,
      goSumSha256: e.BF_GO_SUM_SHA256,
      moduleGraphDigest: e.BF_GO_MODULE_GRAPH_DIGEST,
      moduleCount: Number(e.BF_GO_MODULE_COUNT),
      identity: e.BF_GO_DEPENDENCY_IDENTITY,
      authentication: 'fresh-private-gomodcache-go-sumdb',
    },
    goos: 'linux',
    goarch: e.BF_GOARCH,
    cgoEnabled: true,
    trimpath: true,
    buildvcs: false,
    ...(e.BF_GOARCH_VARIANT ? { goarchVariant: e.BF_GOARCH_VARIANT } : {}),
    go111module: 'on',
    goenv: 'off',
    goflags: '-mod=readonly',
    gowork: 'off',
    gotoolchain: 'local',
    gotelemetry: 'off',
    goproxy: 'https://proxy.golang.org',
    gosumdb: 'sum.golang.org',
    govcs: '*:off',
    tags: e.BF_BUILD_TAGS.split(','),
    panelLdflags: e.BF_PANEL_LDFLAGS,
    helperLdflags: e.BF_HELPER_LDFLAGS,
  },
  cgo: {
    ccIdentity: e.BF_COMPILER_IDENTITY,
    cxxIdentity: e.BF_CXX_IDENTITY,
    ccSha256: e.BF_CC_SHA256,
    cxxSha256: e.BF_CXX_SHA256,
    linkerSha256: e.BF_LLD_SHA256,
    resourceDirectorySha256: e.BF_RESOURCE_DIR_SHA256,
    sysrootSha256: e.BF_SYSROOT_SHA256,
    target: e.BF_CC_TARGET,
    ccArguments: [e.BF_CC_TARGET_ARGUMENT, '--sysroot=<bound-sysroot>'],
    cxxArguments: [e.BF_CXX_TARGET_ARGUMENT, '--sysroot=<bound-sysroot>'],
    cflags: '-O2 -g',
    cppflags: '',
    cxxflags: '-O2 -g',
    fflags: '-O2 -g',
    ldflags: '-fuse-ld=lld',
    pkgConfig: 'disabled',
  },
  cronet: { commit: e.BF_CRONET_COMMIT },
  frontend: {
    node: e.BF_NODE_VERSION,
    nodeExecutableSha256: e.BF_NODE_SHA256,
    npm: e.BF_NPM_VERSION,
    npmExecutableSha256: e.BF_NPM_SHA256,
    install: 'npm ci',
    command: 'SOLOVEY_UI_PROFILE=full npm run build',
    profile: 'full',
    environment: {
      ci: true,
      nodeOptions: '',
      userConfig: 'disabled',
      globalConfig: 'disabled',
      browserslist: 'source-owned',
    },
  },
  generated: { componentImportsProfile: 'full', componentAssetsProfile: 'full' },
  releaseTrustRootsSha256: e.BF_TRUST_ROOTS_SHA256,
  environment: {
    model: 'explicit-allowlist-v1',
    home: 'fresh-private',
    xdgConfig: 'fresh-private',
    temporaryDirectory: 'fresh-private',
    forbiddenTargetSelectors: ['SOLOVEY_UI_COMPONENT_IDS', 'SOLOVEY_UI_PROFILE', 'SUI_E2E', 'SUI_E2E_*', 'VITE_*'],
  },
  reproducibility: { sourceDateEpoch: '0', locale: 'C', timezone: 'UTC', umask: '0022' },
}
fs.writeFileSync(e.BF_OUT, `${JSON.stringify(facts, null, 2)}\n`)
NODE

fingerprint="$temporary_root/SOURCE_FINGERPRINT.json"
run_node "$snapshot/scripts/openwrt-package-source-fingerprint.mjs" create \
	--source-root "$snapshot" --build-facts "$build_facts" --out "$fingerprint"
source_fingerprint=$(run_node -e "const f=require(process.argv[1]); process.stdout.write(f.sourceFingerprint)" "$fingerprint")
build_identity=$(run_node -e "const f=require(process.argv[1]); process.stdout.write(f.build.identity)" "$fingerprint")
version=$(sed -n '1p' "$snapshot/config/identity/version")
sing_box=$(cd "$snapshot" && run_go list -m -f '{{.Version}}' github.com/sagernet/sing-box)

build_info="$temporary_root/BUILD_INFO.txt"
{
	echo 'app=solovey-ui'
	echo "version=$version"
	echo "commit=$commit"
	echo 'profile=full'
	echo "platform=$OPENWRT_PLATFORM"
	echo "target=${OPENWRT_RELEASE}-${OPENWRT_TARGET}-${OPENWRT_SUBTARGET}-${OPENWRT_PACKAGE_ARCH}"
	echo "go=$go_version"
	echo "go_toolchain_identity=$go_toolchain_identity"
	echo "go_dependency_identity=$go_dependency_identity"
	echo "sing_box=$sing_box"
	echo "source_fingerprint=$source_fingerprint"
	echo "build_identity=$build_identity"
} > "$build_info"

# The exact private toolchain and authenticated module cache are reverified at
# the final use boundary. No caller-owned Go root or shared compiled/module
# cache is reachable by the target build that follows.
run_node "$source_dir/scripts/openwrt-go-authority.mjs" verify-toolchain \
	--root "$go_root" --proof "$go_toolchain_proof"
(
	cd "$snapshot"
	run_go mod verify
)
(
	cd "$snapshot"
	run_go list -deps -json -tags "$BUILD_TAGS" . \
		./cmd/solovey-privileged-broker ./cmd/solovey-ssh-proof ./cmd/solovey-evidence ./cmd/solovey-broker-readiness \
		./cmd/solovey-openwrt-preservation ./components/server-protection/cmd/solovey-openwrt-owner-manifest \
		./cmd/solovey-openwrt-broker-manifest ./cmd/solovey-openwrt-durability ./cmd/solovey-openwrt-lifecycle
) | run_node "$source_dir/scripts/openwrt-go-authority.mjs" verify-dependencies \
	--graph package --go-mod "$snapshot/go.mod" --go-sum "$snapshot/go.sum" --proof "$go_dependency_proof"
go_environment+=('SOLOVEY_GO_AUTHORITY_VERIFIED=1')
(
	cd "$snapshot"
env -i "${go_environment[@]}" bash scripts/build-linux-target.sh --mode openwrt --out "$binary_dir" \
		--platform "$OPENWRT_PLATFORM" --trust-roots-file "$trust_roots_file"
)

payload="$out_dir/payload"
install -d -m 0755 "$payload/usr/lib/solovey-ui/components" "$payload/etc/init.d" \
	"$payload/lib/upgrade/keep.d" "$payload/usr/share/licenses/solovey-ui"
for binary in solovey-ui solovey-privileged-broker solovey-ssh-proof solovey-evidence solovey-broker-readiness \
	solovey-openwrt-preservation solovey-openwrt-owner-manifest solovey-openwrt-broker-manifest \
	solovey-openwrt-durability solovey-openwrt-lifecycle; do
	install -m 0755 "$binary_dir/$binary" "$payload/usr/lib/solovey-ui/$binary"
done
install -m 0644 "$build_info" "$payload/usr/lib/solovey-ui/BUILD_INFO.txt"
install -m 0644 "$fingerprint" "$payload/usr/lib/solovey-ui/SOURCE_FINGERPRINT.json"
install -m 0644 "$go_toolchain_proof" "$payload/usr/lib/solovey-ui/GO_TOOLCHAIN_AUTHORITY.json"
install -m 0644 "$go_dependency_proof" "$payload/usr/lib/solovey-ui/GO_DEPENDENCY_AUTHORITY.json"
cp -a "$snapshot/.release/components/." "$payload/usr/lib/solovey-ui/components/"
find "$payload/usr/lib/solovey-ui/components" -type d -exec chmod 0755 {} +
find "$payload/usr/lib/solovey-ui/components" -type f -exec chmod 0644 {} +
install -m 0755 "$snapshot/deploy/openwrt/solovey-ui.init" "$payload/etc/init.d/solovey-ui"
install -m 0755 "$snapshot/deploy/openwrt/solovey-openwrt-prepare" "$payload/usr/lib/solovey-ui/solovey-openwrt-prepare"
install -m 0755 "$snapshot/deploy/openwrt/solovey-ui-upgrade.sh" "$payload/lib/upgrade/solovey-ui.sh"
install -m 0644 "$snapshot/deploy/openwrt/solovey-ui.keep" "$payload/lib/upgrade/keep.d/solovey-ui"
install -m 0644 "$snapshot/LICENSE" "$payload/usr/share/licenses/solovey-ui/LICENSE"

run_node "$snapshot/scripts/openwrt-stage-manifest.mjs" create --stage "$out_dir" --source-root "$source_dir"
run_node "$snapshot/scripts/openwrt-stage-manifest.mjs" verify --stage "$out_dir" --source-root "$source_dir"
echo "[openwrt-stage-build] canonical stage: $out_dir"
