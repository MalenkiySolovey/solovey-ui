#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
# Real chmod/chown, trusted ancestry and manifest publication; root execution is
# routed by the same capability executor used by CI, Audit and release gates.
node scripts/go-test.mjs -count=1 -run '^TestInstaller(SSHProof|Component)OwnershipContract$' -- ./cmd/solovey-broker-manifest
node scripts/go-test.mjs -count=1 -- ./components/server-protection/cmd/solovey-owner-manifest
bash tests/installer/native-transaction.sh
probe_root=$(mktemp -d)
trap 'rm -rf "$probe_root"' EXIT
go test -c -o "$probe_root/component-api.test" ./api
if (( EUID == 0 )); then
  (cd api; SOLOVEY_TEST_NATIVE_COMPONENTS=1 "$probe_root/component-api.test" -test.run '^TestNativeInventoryServiceUIDMigrationAndRoutes$')
else
  (cd api; sudo -n env SOLOVEY_TEST_NATIVE_COMPONENTS=1 "$probe_root/component-api.test" -test.run '^TestNativeInventoryServiceUIDMigrationAndRoutes$')
fi
