#!/usr/bin/env bash

set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

ASSETS="${TMP}/assets"
COMPONENTS="${TMP}/components"
OUT="${ASSETS}/solovey-ui-release.json"
mkdir -p "${ASSETS}" "${COMPONENTS}/telegram"

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

printf 'full\n' > "${ASSETS}/solovey-ui-linux-amd64.tar.gz"
printf 'core\n' > "${ASSETS}/solovey-ui-core-linux-amd64.tar.gz"
printf 'components\n' > "${ASSETS}/solovey-ui-components.tar.gz"
printf '{"id":"telegram","version":"1","delivery":"in-process"}\n' > "${COMPONENTS}/telegram/component.json"

PRIVATE_KEY_B64="$(node -e "const {generateKeyPairSync}=require('node:crypto');const {privateKey}=generateKeyPairSync('ed25519');process.stdout.write(privateKey.export({format:'der',type:'pkcs8'}).toString('base64'))")"
export PRIVATE_KEY_B64
export SUI_RELEASE_SIGNING_PRIVATE_KEY_B64="${PRIVATE_KEY_B64}"

cd "${ROOT}"
node scripts/release-manifest.mjs \
    --version "$(tr -d '\r\n' < config/identity/version)" \
    --sequence 123 \
    --channel main \
    --issued-at 2000000000 \
    --key-id test-key \
    --assets-dir "${ASSETS}" \
    --components-dir "${COMPONENTS}" \
    --out "${OUT}" >/dev/null

(
    cd "${ASSETS}"
    sha256sum -c "$(basename "${OUT}").sha256" >/dev/null
)

node - "${OUT}" <<'NODE'
const crypto = require('node:crypto')
const fs = require('node:fs')

const envelope = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'))
if (envelope.schema !== 'solovey.release/v1') throw new Error('unexpected envelope schema')
if (envelope.algorithm !== 'Ed25519') throw new Error('unexpected signature algorithm')
if (envelope.manifest.artifacts.length !== 3) throw new Error('unexpected artifact count')
if (envelope.manifest.components.length !== 1 || envelope.manifest.components[0].id !== 'telegram') {
  throw new Error('unexpected component inventory')
}
const privateKey = crypto.createPrivateKey({
  key: Buffer.from(process.env.PRIVATE_KEY_B64, 'base64'),
  format: 'der',
  type: 'pkcs8',
})
const publicKey = crypto.createPublicKey(privateKey)
const canonical = Buffer.from(JSON.stringify(envelope.manifest))
if (!crypto.verify(null, canonical, publicKey, Buffer.from(envelope.signature, 'base64'))) {
  throw new Error('release manifest signature verification failed')
}
NODE

rm -f "${ASSETS}/solovey-ui-core-linux-amd64.tar.gz" "${OUT}" "${OUT}.sha256"
if node scripts/release-manifest.mjs \
    --version "$(tr -d '\r\n' < config/identity/version)" \
    --sequence 124 \
    --channel main \
    --issued-at 2000000000 \
    --key-id test-key \
    --assets-dir "${ASSETS}" \
    --components-dir "${COMPONENTS}" \
    --out "${OUT}" >/dev/null 2>&1; then
    fail "manifest generation accepted an asymmetric full/core release set"
fi

printf 'PASS: signed release manifest integration\n'
