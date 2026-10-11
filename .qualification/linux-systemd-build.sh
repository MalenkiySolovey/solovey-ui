#!/usr/bin/env bash
# Goal adaptation of release.yml and run-generic-linux-candidates.sh.
# Canonical build/package owners are unchanged; nothing is published.
set -euo pipefail
test "$(git rev-parse HEAD)" = "${W5_PRODUCT_COMMIT:?}"
test "$(go env GOVERSION)" = go1.26.9
arch=${W5_TARGET_ARCH:?}
[[ "$arch" == amd64 || "$arch" == arm64 ]]
test -n "${SUI_RELEASE_TRUST_ROOTS_B64:?}"
export CGO_ENABLED=1 GOOS=linux GOARCH="$arch" GOWORK=off
fingerprint=$(node scripts/release-source.mjs .)
mkdir -p qualification-evidence frontend-contract/full frontend-contract/core frontend-contract/components
node scripts/generate-component-imports.mjs --profile full --out app/components_generated.go --cmd-out cmd/optional_commands_generated.go
(cd frontend; npm ci; SOLOVEY_UI_PROFILE=full npm run build)
node scripts/check-frontend-profile.mjs --profile full --dist frontend/dist
node scripts/extract-component-frontend.mjs --dist frontend/dist --components-dir components --out-dir frontend-contract/components --prune-dist
node scripts/write-component-installed-metadata.mjs --components-dir frontend-contract/components --out frontend-contract/components/installed.json --profile full --binary full
node scripts/frontend-assets.mjs verify --dist frontend/dist --components-dir frontend-contract/components
cp -a frontend/dist/. frontend-contract/full/
(cd frontend; SOLOVEY_UI_PROFILE=core npm run build)
node scripts/check-frontend-profile.mjs --profile core --dist frontend/dist
cp -a frontend/dist/. frontend-contract/core/
printf '%s' "$SUI_RELEASE_TRUST_ROOTS_B64" > "$RUNNER_TEMP/w5-public-trust-roots.b64"
for profile in full core; do
 output="qualification-build/$profile"
 if [[ "$profile" == full ]]; then
  node scripts/frontend-assets.mjs publish --dist frontend-contract/full --destination web/html --components-dir frontend-contract/components
 else
  node scripts/frontend-assets.mjs publish --dist frontend-contract/core --destination web/html
 fi
 bash scripts/build-linux-target.sh --mode release --platform "$arch" --profile "$profile" --naive --out "$output" --trust-roots-file "$RUNNER_TEMP/w5-public-trust-roots.b64"
 binary="$output/solovey-ui"
 [[ "$profile" != core ]] || binary="$output/solovey-ui-core"
 go version -m "$binary" > "qualification-evidence/GO_BUILD_INFO_${profile}.txt"
 {
  printf 'app=solovey-ui\nversion=v2026.3.3\ncommit=%s\nsource_fingerprint=%s\nplatform=linux/%s\nprofile=%s\n' "$W5_PRODUCT_COMMIT" "$fingerprint" "$arch" "$profile"
  printf 'go=%s\nsing_box=v1.14.2\ncompiler=%s\n' "$(go version)" "$CC"
  node scripts/quic-integration-provenance.mjs build-info
  node scripts/ssm-integration-provenance.mjs build-info
  node scripts/anytls-integration-provenance.mjs build-info
 } > "$output/BUILD_INFO.txt"
 full_helpers=qualification-build/full
 args=(--profile "$profile" --target "linux-$arch" --binary "$binary" --broker "$output/solovey-privileged-broker"
  --proof "$full_helpers/solovey-ssh-proof" --manifest-writer "$full_helpers/solovey-broker-manifest"
  --manager ./solovey-ui.sh --service ./solovey-ui.service --build-info "$output/BUILD_INFO.txt" --out-dir qualification-evidence)
 [[ "$profile" != full ]] || args+=(--owner-manifest-writer "$output/solovey-owner-manifest")
 bash scripts/release-package-linux.sh "${args[@]}"
done
test "$(node scripts/release-source.mjs .)" = "$fingerprint"
