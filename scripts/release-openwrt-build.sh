#!/usr/bin/env bash
# Execution adapter only: all target/package semantics belong to tagged source.
set -Eeuo pipefail
profile=${1:?profile}
product=${2:?product commit}
[[ "$(git rev-parse HEAD)" == "$product" ]]
git diff --exit-code HEAD --
source scripts/openwrt-target-profile.sh
openwrt_target_profile_load "$profile"
work="$RUNNER_TEMP/openwrt-$profile"
mkdir -p "$work/downloads" "$work/go"
curl --fail --location --retry 4 --retry-all-errors --proto '=https' --tlsv1.2 \
  "$OPENWRT_SDK_DOWNLOAD" -o "$work/downloads/$OPENWRT_SDK_FILENAME"
printf '%s  %s\n' "$OPENWRT_SDK_SHA256" "$work/downloads/$OPENWRT_SDK_FILENAME" | sha256sum -c -
# Read the canonical producer's Go archive authority, not setup-go defaults.
go_file=$(sed -n "s/^readonly GO_TOOLCHAIN_FILENAME='\([^']*\)'/\1/p" scripts/openwrt-stage-build.sh)
go_sha=$(sed -n "s/^readonly GO_TOOLCHAIN_SHA256='\([^']*\)'/\1/p" scripts/openwrt-stage-build.sh)
[[ "$go_file" =~ ^go[0-9.]+\.linux-amd64\.tar\.gz$ && "$go_sha" =~ ^[a-f0-9]{64}$ ]]
curl --fail --location --retry 4 --retry-all-errors --proto '=https' --tlsv1.2 \
  "https://go.dev/dl/$go_file" -o "$work/downloads/$go_file"
printf '%s  %s\n' "$go_sha" "$work/downloads/$go_file" | sha256sum -c -
tar -xzf "$work/downloads/$go_file" -C "$work/go" --no-same-owner
cronet_commit=$(sed -n "s/^readonly CRONET_TOOLCHAIN_COMMIT='\([^']*\)'/\1/p" scripts/openwrt-stage-build.sh)
git init "$work/cronet"
git -C "$work/cronet" remote add origin https://github.com/sagernet/cronet-go.git
git -C "$work/cronet" fetch --depth=1 origin "$cronet_commit"
git -C "$work/cronet" checkout --detach FETCH_HEAD
[[ "$(git -C "$work/cronet" rev-parse HEAD)" == "$cronet_commit" ]]
git -C "$work/cronet" submodule update --init --recursive --depth=1
export PATH="$work/go/go/bin:$PATH"
(
  cd "$work/cronet"
  go run ./cmd/build-naive --target="linux/$OPENWRT_GOARCH" --libc=musl download-toolchain
  go run ./cmd/build-naive --target="linux/$OPENWRT_GOARCH" --libc=musl env > "$work/compiler.env"
)
# This is the pinned project's existing toolchain environment producer.
source "$work/compiler.env"
printf '%s' "$SUI_RELEASE_TRUST_ROOTS_B64" > "$work/public-roots.b64"
# Generated imports are ignored checkout outputs. Materialize using the tagged
# generator; the producer then checks and independently regenerates its snapshot.
node scripts/generate-component-imports.mjs --profile full
env -i PATH="$PATH" HOME="$HOME" CC="$CC" CXX="$CXX" \
  /usr/bin/bash scripts/openwrt-package-build.sh \
    --sdk-archive "$work/downloads/$OPENWRT_SDK_FILENAME" \
    --go-toolchain-archive "$work/downloads/$go_file" \
    --source "$PWD" --trust-roots-file "$work/public-roots.b64" \
    --out "$work/package" --jobs 2 --target-profile "$profile"
node "$RUNNER_TEMP/release-openwrt.mjs" prepare "$work/package" "$product" "$profile" openwrt-candidate
