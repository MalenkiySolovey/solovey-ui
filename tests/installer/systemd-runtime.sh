#!/usr/bin/env bash
set -euo pipefail

# Real kernel/systemd execution: absence of an executor is an explicit nonzero
# boundary, never a passing skip. CI runs this on its systemd Linux host.
if [[ $(cat /proc/1/comm) != systemd ]]; then
  echo 'REAL_SYSTEMD_NETLINK_PROOF=EXECUTOR_UNAVAILABLE' >&2
  exit 77
fi
repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repository_root"
node scripts/generate-systemd-policy.mjs --check
build_root=$(mktemp -d)
probe_root="/run/solovey-netlink-proof-$$"
privileged=()
if (( EUID != 0 )); then privileged=(sudo -n); fi
cleanup() {
  "${privileged[@]}" rm -rf -- "$probe_root"
  rm -rf -- "$build_root"
}
trap cleanup EXIT
CGO_ENABLED=0 go build -o "$build_root/probe" tests/installer/systemd-runtime-probe.go
# PrivateTmp hides /tmp and /var/tmp; ProtectHome hides runner home directories.
# Install only this diagnostic binary in a unique root-owned runtime directory.
"${privileged[@]}" install -d -m 0755 "$probe_root"
"${privileged[@]}" install -m 0755 "$build_root/probe" "$probe_root/probe"
properties=()
while IFS= read -r line; do
  line=${line%$'\r'}
  case "$line" in
    NoNewPrivileges=*|PrivateTmp=*|PrivateDevices=*|Protect*=*|Restrict*=*|LockPersonality=*|MemoryDenyWriteExecute=*|SystemCallArchitectures=*|CapabilityBoundingSet=*|AmbientCapabilities=*|UMask=*)
      properties+=(--property="$line");;
  esac
done < deploy/systemd/solovey-ui-native-hardened.service
# Use the executor's pre-existing unprivileged account. Product User/Group and
# empty capability policy are independently asserted by deployment tests.
"${privileged[@]}" systemd-run --quiet --wait --pipe --collect \
  --unit="solovey-netlink-fixed-$$" --property=User=nobody --property=Group=nogroup \
  "${properties[@]}" "$probe_root/probe"
# Replace, do not append: repeated RestrictAddressFamilies directives accumulate.
for i in "${!properties[@]}"; do
  if [[ ${properties[$i]} == --property=RestrictAddressFamilies=* ]]; then
    properties[$i]='--property=RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX'
  fi
done
"${privileged[@]}" systemd-run --quiet --wait --pipe --collect \
  --unit="solovey-netlink-original-$$" --property=User=nobody --property=Group=nogroup \
  "${properties[@]}" "$probe_root/probe" blocked
