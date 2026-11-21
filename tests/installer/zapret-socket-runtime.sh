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
socket_root=$("${privileged[@]}" mktemp -d /run/sui-z.XXXXXXXX)
generation=$(printf '2%.0s' {1..64})
early_generation=$(printf '3%.0s' {1..64})
prefix="solovey-zsocket-${root##*.}"
panel="$prefix-panel.service"
wrong="$prefix-other.service"
broker="$prefix-broker.service"
base_guard="$prefix-bpf-base.service"
scoped_guard="$prefix-bpf-scoped.service"
packet_hooks="$prefix-packet-hooks.service"
cleanup() {
  "${privileged[@]}" journalctl -u "$panel" -u "$wrong" -u "$broker" -u "$base_guard" -u "$scoped_guard" -u "$packet_hooks" --no-pager -o cat || true
  "${privileged[@]}" systemctl stop "$panel" "$wrong" "$broker" "$base_guard" "$scoped_guard" "$packet_hooks" 2>/dev/null || true
  "${privileged[@]}" ip netns delete "$prefix" 2>/dev/null || true
  "${privileged[@]}" umount "$root/pins" 2>/dev/null || true
  "${privileged[@]}" rm -f -- "$socket_root/z-$generation" "$socket_root/z-$early_generation" "$root/peer.test"
  "${privileged[@]}" rmdir -- "$root/pins" "$socket_root" "$root" 2>/dev/null || true
  rm -f -- "$build/peer.test"
  rmdir -- "$build"
}
trap cleanup EXIT
go test -c -o "$build/peer.test" ./internal/ops/privilegedbroker
"${privileged[@]}" chmod 0755 "$root"
"${privileged[@]}" chown 0:65534 "$socket_root"
"${privileged[@]}" chmod 0750 "$socket_root"
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
        if [[ $name == "$scoped_guard" || $name == "$packet_hooks" ]]; then line+=' CAP_BPF'; fi
        properties+=(--property="$line");;
      NoNewPrivileges=*|PrivateTmp=*|PrivateDevices=*|Protect*=*|Restrict*=*|LockPersonality=*|MemoryDenyWriteExecute=*|SystemCallArchitectures=*|UMask=*)
        properties+=(--property="$line");;
    esac
  done < "deploy/systemd/$profile.service"
  "${privileged[@]}" systemd-run --quiet --unit="$name" --service-type=exec --remain-after-exit \
    --property=RuntimeMaxSec=35s --property=WorkingDirectory="$root" \
    --property=NetworkNamespacePath="/run/netns/$prefix" --property=User="$uid" --property=Group="$uid" \
    "${properties[@]}" --setenv="SOLOVEY_TEST_ZAPRET_SOCKET_ROOT=$socket_root" \
    --setenv="SOLOVEY_TEST_ZAPRET_GENERATION=$generation" \
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
  --property=ReadWritePaths="$socket_root" --setenv="SOLOVEY_TEST_ZAPRET_PEER_UNIT=$panel" \
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

"${privileged[@]}" ip netns exec "$prefix" sysctl -qw net.ipv6.conf.all.disable_ipv6=1
"${privileged[@]}" ip -n "$prefix" link add zprobe2 type veth peer name zprobe3
"${privileged[@]}" ip -n "$prefix" link set zproof1 address 02:00:00:08:00:01
for iface in zproof0 zproof1 zprobe2 zprobe3; do
  "${privileged[@]}" ip -n "$prefix" link set "$iface" up
done
"${privileged[@]}" ip -n "$prefix" addr add 192.0.2.2/24 dev zproof0
"${privileged[@]}" ip -n "$prefix" route add 198.51.100.25/32 dev zproof0
"${privileged[@]}" ip -n "$prefix" neigh add 198.51.100.25 lladdr 02:00:00:08:00:01 nud permanent dev zproof0
"${privileged[@]}" ip netns exec "$prefix" tc qdisc add dev zproof0 clsact
"${privileged[@]}" ip netns exec "$prefix" tc filter add dev zproof0 egress protocol ip pref 10 matchall action mirred egress redirect dev zprobe2
run_profile "$packet_hooks" solovey-privileged-broker 0 TestSystemdZapretPacketHookCandidates \
  --property=ReadWritePaths="$root/pins" --setenv="SOLOVEY_TEST_ZAPRET_PACKET_HOOKS=$root/pins"
wait_profile "$packet_hooks"
