#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
# Real chmod/chown, trusted ancestry and manifest publication; root execution is
# routed by the same capability executor used by CI, Audit and release gates.
node scripts/go-test.mjs -count=1 -run '^TestInstallerSSHProofOwnershipContract$' -- ./cmd/solovey-broker-manifest
