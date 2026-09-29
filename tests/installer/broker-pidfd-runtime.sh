#!/usr/bin/env bash
set -euo pipefail
if [[ $(cat /proc/1/comm) != systemd ]]; then
  echo 'REAL_CROSS_UID_PIDFD_PROOF=EXECUTOR_UNAVAILABLE' >&2
  exit 77
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
build_root=$(mktemp -d)
probe_root="/run/solovey-pidfd-proof-$$"
privileged=()
if (( EUID != 0 )); then privileged=(sudo -n); fi
cleanup() {
  "${privileged[@]}" rm -rf -- "$probe_root"
  rm -rf -- "$build_root"
}
trap cleanup EXIT
go test -c -o "$build_root/peer.test" ./internal/ops/privilegedbroker
"${privileged[@]}" install -d -m 0755 "$probe_root"
"${privileged[@]}" install -m 0755 "$build_root/peer.test" "$probe_root/peer.test"
properties=()
while IFS= read -r line; do
  line=${line%$'\r'}
  case "$line" in
    NoNewPrivileges=*|PrivateTmp=*|PrivateDevices=*|Protect*=*|Restrict*=*|LockPersonality=*|MemoryDenyWriteExecute=*|SystemCallArchitectures=*|CapabilityBoundingSet=*|AmbientCapabilities=*|UMask=*)
      properties+=(--property="$line");;
  esac
done < deploy/systemd/solovey-privileged-broker.service
unit="solovey-pidfd-proof-$$.service"
"${privileged[@]}" systemd-run --quiet --wait --pipe --collect --unit="$unit" \
  --property=User=root --property=Group=root "${properties[@]}" \
  --setenv="SOLOVEY_TEST_PIDFD_UNIT=$unit" \
  "$probe_root/peer.test" -test.v -test.timeout=45s -test.run='^TestSystemdCrossUIDPidfdAttestation$'
