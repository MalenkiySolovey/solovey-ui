#!/usr/bin/env bash
set -euo pipefail

if [[ ${GITHUB_ACTIONS:-} != true || $(cat /proc/1/comm) != systemd ]]; then
  echo 'ZAPRET_SOCKET_SERVICE_PROOF=DISPOSABLE_SYSTEMD_RUNNER_REQUIRED' >&2
  exit 77
fi
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
privileged=()
if (( EUID != 0 )); then privileged=(sudo -n); fi
build=$(mktemp -d)
root=$("${privileged[@]}" mktemp -d /usr/local/lib/sui-zsocket.XXXXXXXX)
prefix="solovey-zsocket-${root##*.}"
panel="$prefix-panel.service"
wrong="$prefix-other.service"
broker="$prefix-broker.service"
base_guard="$prefix-bpf-base.service"
scoped_guard="$prefix-bpf-scoped.service"
cleanup() {
  "${privileged[@]}" journalctl -u "$panel" -u "$wrong" -u "$broker" -u "$base_guard" -u "$scoped_guard" --no-pager -o cat || true
  "${privileged[@]}" systemctl stop "$panel" "$wrong" "$broker" "$base_guard" "$scoped_guard" 2>/dev/null || true
  "${privileged[@]}" ip netns delete "$prefix" 2>/dev/null || true
  "${privileged[@]}" umount "$root/pins" 2>/dev/null || true
  "${privileged[@]}" rm -f -- "$root/socket/admit.sock" "$root/peer.test"
  "${privileged[@]}" rmdir -- "$root/pins" "$root/socket" "$root" 2>/dev/null || true
  rm -f -- "$build/peer.test"
  rmdir -- "$build"
}
trap cleanup EXIT
go test -c -o "$build/peer.test" ./internal/ops/privilegedbroker
"${privileged[@]}" chmod 0755 "$root"
"${privileged[@]}" install -d -o 0 -g 65534 -m 0750 "$root/socket"
"${privileged[@]}" install -m 0755 "$build/peer.test" "$root/peer.test"
"${privileged[@]}" ip netns add "$prefix"
"${privileged[@]}" ip -n "$prefix" link add zproof0 type veth peer name zproof1
"${privileged[@]}" install -d -o 0 -g 0 -m 0700 "$root/pins"
"${privileged[@]}" mount -t bpf bpf -o mode=0700 "$root/pins"

run_profile() {
  local name=$1 profile=$2 uid=$3 test=$4
  shift 4
  local line
  local properties=()
  while IFS= read -r line; do
    line=${line%$'\r'}
    case "$line" in
      CapabilityBoundingSet=*|AmbientCapabilities=*)
        if [[ $name == "$scoped_guard" ]]; then line+=' CAP_BPF'; fi
        properties+=(--property="$line");;
      NoNewPrivileges=*|PrivateTmp=*|PrivateDevices=*|Protect*=*|Restrict*=*|LockPersonality=*|MemoryDenyWriteExecute=*|SystemCallArchitectures=*|UMask=*)
        properties+=(--property="$line");;
    esac
  done < "deploy/systemd/$profile.service"
  "${privileged[@]}" systemd-run --quiet --unit="$name" --service-type=exec --remain-after-exit \
    --property=RuntimeMaxSec=35s --property=WorkingDirectory="$root" \
    --property=NetworkNamespacePath="/run/netns/$prefix" --property=User="$uid" --property=Group="$uid" \
    "${properties[@]}" --setenv="SOLOVEY_TEST_ZAPRET_SOCKET=$root/socket/admit.sock" \
    --setenv="SOLOVEY_TEST_ZAPRET_PARENT_NETNS=$(readlink /proc/self/ns/net)" \
    "$@" "$root/peer.test" -test.v -test.timeout=30s -test.run="^${test}$"
}

wait_profile() {
  local unit=$1 state attempt
  for ((attempt=0; attempt<120; attempt++)); do
    state=$("${privileged[@]}" systemctl show "$unit" -p SubState --value)
    [[ $state == running || $state == start ]] || break
    sleep 0.25
  done
  [[ $state == exited && $("${privileged[@]}" systemctl show "$unit" -p ExecMainStatus --value) == 0 ]]
}

run_profile "$panel" solovey-ui-native-hardened 65534 TestSystemdZapretSocketProvider
pid=$("${privileged[@]}" systemctl show "$panel" -p MainPID --value)
[[ $pid =~ ^[0-9]+$ && $pid -gt 1 ]]
run_profile "$broker" solovey-privileged-broker 0 TestSystemdZapretSocketBroker \
  --property=ReadWritePaths="$root/socket" --setenv="SOLOVEY_TEST_ZAPRET_PEER_UNIT=$panel" \
  --setenv="SOLOVEY_TEST_ZAPRET_PEER_PID=$pid"
run_profile "$wrong" solovey-ui-native-hardened 65534 TestSystemdZapretSocketProvider \
  --setenv=SOLOVEY_TEST_ZAPRET_WRONG_UNIT=1
for unit in "$panel" "$wrong" "$broker"; do
  wait_profile "$unit"
done
run_profile "$base_guard" solovey-privileged-broker 0 TestSystemdZapretBrokerGuardCapabilities \
  --property=ReadWritePaths="$root/pins" --setenv="SOLOVEY_TEST_ZAPRET_GUARD_PATH=$root/pins"
wait_profile "$base_guard"
run_profile "$scoped_guard" solovey-privileged-broker 0 TestSystemdZapretBrokerGuardCapabilities \
  --property=ReadWritePaths="$root/pins" --setenv="SOLOVEY_TEST_ZAPRET_GUARD_PATH=$root/pins" \
  --setenv=SOLOVEY_TEST_ZAPRET_CAP_BPF=1
wait_profile "$scoped_guard"
