#!/usr/bin/env bash
set -euo pipefail
if [[ $(cat /proc/1/comm) != systemd ]]; then
  echo 'REAL_REQUEST_TRANSPORT=EXECUTOR_UNAVAILABLE' >&2
  exit 77
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
privileged=()
if (( EUID != 0 )); then privileged=(sudo -n); fi
# Production clients enforce fixed paths. Never take over an installed broker.
if [[ -e /run/solovey-ui ]]; then
  echo 'REAL_REQUEST_TRANSPORT=FIXED_PATH_IN_USE' >&2
  exit 77
fi
build_root=$(mktemp -d)
probe_root="/run/solovey-request-proof-$$"
prefix="solovey-request-proof-$$"
broker="$prefix.service"
client="$prefix-client.service"
main="$prefix-main.socket"
proof="$prefix-proof.socket"
cleanup() {
  "${privileged[@]}" systemctl stop "$client" "$broker" "$main" "$proof" 2>/dev/null || true
  "${privileged[@]}" systemctl reset-failed "$broker" 2>/dev/null || true
  "${privileged[@]}" rm -f -- "/run/systemd/system/$broker" "/run/systemd/system/$main" "/run/systemd/system/$proof"
  "${privileged[@]}" systemctl daemon-reload
  "${privileged[@]}" rm -rf -- "$probe_root"
  "${privileged[@]}" rmdir /run/solovey-ui 2>/dev/null || true
  rm -rf -- "$build_root"
}
trap cleanup EXIT
go test -c -o "$build_root/peer.test" ./internal/ops/privilegedbroker
"${privileged[@]}" install -d -m 0755 "$probe_root"
"${privileged[@]}" install -d -o root -g 65534 -m 0750 /run/solovey-ui
"${privileged[@]}" install -m 0755 "$build_root/peer.test" "$probe_root/peer.test"
for role in main proof; do
  template=deploy/systemd/solovey-privileged-broker.socket
  [[ $role == main ]] || template=deploy/systemd/solovey-privileged-proof.socket
  sed -e 's/SocketGroup=solovey-ui/SocketGroup=65534/' \
    -e "s/Service=solovey-privileged-broker.service/Service=$broker/" "$template" > "$build_root/$prefix-$role.socket"
  "${privileged[@]}" install -m 0644 "$build_root/$prefix-$role.socket" "/run/systemd/system/$prefix-$role.socket"
done
{
  printf '[Unit]\nRequires=%s %s\nAfter=%s %s\n[Service]\nType=exec\nUser=root\nGroup=root\n' "$main" "$proof" "$main" "$proof"
  printf 'Environment=SOLOVEY_TEST_REQUEST_CLIENT_UNIT=%s\n' "$client"
  printf 'ExecStart=%s/peer.test -test.v -test.timeout=30s -test.run=^TestSystemdRequestBroker$\n' "$probe_root"
  while IFS= read -r line; do
    line=${line%$'\r'}
    case "$line" in
      NoNewPrivileges=*|PrivateTmp=*|PrivateDevices=*|Protect*=*|Restrict*=*|LockPersonality=*|MemoryDenyWriteExecute=*|SystemCallArchitectures=*|CapabilityBoundingSet=*|AmbientCapabilities=*|UMask=*) printf '%s\n' "$line";;
    esac
  done < deploy/systemd/solovey-privileged-broker.service
} > "$build_root/$broker"
"${privileged[@]}" install -m 0644 "$build_root/$broker" "/run/systemd/system/$broker"
"${privileged[@]}" systemctl daemon-reload
for cycle in 1 2 3; do
  echo "SYSTEMD_ACTIVATION_CYCLE=$cycle"
  "${privileged[@]}" systemctl start "$main" "$proof"
  set +e
  "${privileged[@]}" systemd-run --quiet --wait --pipe --collect --unit="$client" \
    --property=User=65534 --property=Group=65534 --property=NoNewPrivileges=true \
    --setenv=SOLOVEY_TEST_REQUEST_CLIENT=1 \
    "$probe_root/peer.test" -test.v -test.timeout=25s -test.run='^TestSystemdRequestClient$'
  client_result=$?
  set -e
  for ((attempt=0; attempt<25; attempt++)); do
    state=$("${privileged[@]}" systemctl show "$broker" -p ActiveState --value)
    [[ $state == active || $state == activating ]] || break
    sleep 1
  done
  "${privileged[@]}" journalctl -u "$broker" --no-pager -o cat
  broker_result=$("${privileged[@]}" systemctl show "$broker" -p ExecMainStatus --value)
  [[ $client_result == 0 && $broker_result == 0 && $state == inactive ]]
  "${privileged[@]}" systemctl stop "$main" "$proof" "$broker"
done
echo 'REAL_CROSS_UID_FULL_PROTOCOL=PASS; ACTIVATION_CYCLES=3; REQUESTS=24'
