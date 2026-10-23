#!/usr/bin/env bash

set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${ROOT}"

PROVIDER=sing-box
while (($#)); do
    case "$1" in
        --provider)
            [[ $# -ge 2 && -n "$2" ]] || { printf 'Missing --provider value\n' >&2; exit 1; }
            PROVIDER="$2"
            shift 2
            ;;
        --help)
            printf 'Usage: build.sh [--provider sing-box|shtorm-extended]\n'
            exit 0
            ;;
        *) printf 'Unknown build argument: %s\n' "$1" >&2; exit 1 ;;
    esac
done
BUILD_TAGS="with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_tailscale"
BUILD_TAGS="$(node scripts/core-provider-build.mjs tags --provider "$PROVIDER" --tags "$BUILD_TAGS")"
PROVIDER_GO_ARGS="$(node scripts/core-provider-build.mjs go-args --provider "$PROVIDER")"
mapfile -t provider_go_args <<< "$PROVIDER_GO_ARGS"

(
    cd frontend
    npm ci
    SOLOVEY_UI_PROFILE=full npm run build
)
node scripts/check-frontend-profile.mjs --profile full --dist frontend/dist
node scripts/generate-component-imports.mjs --profile full --out app/components_generated.go

printf 'Building backend\n'
node scripts/frontend-assets.mjs publish --dist frontend/dist --destination web/html

go build "${provider_go_args[@]}" -ldflags '-w -s -checklinkname=0' -tags "${BUILD_TAGS}" -o solovey-ui main.go
