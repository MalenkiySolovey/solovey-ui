#!/usr/bin/env bash

set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${ROOT}"

(
    cd frontend
    npm ci
    SOLOVEY_UI_PROFILE=full npm run build
)
node scripts/check-frontend-profile.mjs --profile full --dist frontend/dist
node scripts/generate-component-imports.mjs --profile full --out app/components_generated.go

printf 'Building backend\n'
mkdir -p web/html
find web/html -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
cp -R frontend/dist/. web/html/

BUILD_TAGS="with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_tailscale"
go build -ldflags '-w -s -checklinkname=0' -tags "${BUILD_TAGS}" -o solovey-ui main.go
