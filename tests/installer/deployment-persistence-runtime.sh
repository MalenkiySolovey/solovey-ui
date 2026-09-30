#!/usr/bin/env bash
set -euo pipefail
# Real native observation -> activated broker -> authenticated API -> SQLite.
# A temporary systemd DynamicUser reservation supplies the real product identity
# without creating or changing persistent host accounts.
[[ $(cat /proc/1/comm) == systemd && $EUID == 0 ]] || { echo 'REAL_DEPLOYMENT_PERSISTENCE=EXECUTOR_UNAVAILABLE'; exit 77; }
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
roots=(/run/solovey-ui /etc/solovey-ui /usr/local/lib/solovey-ui /usr/local/solovey-ui /var/lib/solovey-ui /var/lib/solovey-ui-broker)
units=(solovey-ui.service solovey-privileged-broker.service solovey-privileged-broker.socket solovey-privileged-proof.socket)
for path in "${roots[@]}" /etc/systemd/system/solovey-ui.service /etc/systemd/system/multi-user.target.wants/solovey-ui.service; do
 [[ ! -e $path && ! -L $path ]] || { echo 'REAL_DEPLOYMENT_PERSISTENCE=FIXED_PATH_IN_USE'; exit 77; }
done
for unit in "${units[@]}"; do
 [[ $(systemctl show "$unit" -p LoadState --value) == not-found ]] || { echo 'REAL_DEPLOYMENT_PERSISTENCE=UNIT_IN_USE'; exit 77; }
done
build_root=$(mktemp -d)
identity_unit="solovey-deployment-identity-$$.service"
cleanup() {
 systemctl stop "${units[@]}" 2>/dev/null || true
 systemctl reset-failed "${units[@]}" 2>/dev/null || true
 for unit in "${units[@]}"; do rm -f -- "/run/systemd/system/$unit"; done
 rm -f -- /etc/systemd/system/solovey-ui.service
 rm -f -- /etc/systemd/system/multi-user.target.wants/solovey-ui.service
 systemctl daemon-reload
 # Every root was proven absent before fixture creation.
 rm -rf -- "${roots[@]}" "$build_root"
 systemctl stop "$identity_unit" 2>/dev/null || true
}
trap cleanup EXIT
go test -c -o "$build_root/api.test" ./api
systemd-run --quiet --collect --unit="$identity_unit" --property=DynamicUser=yes --property=User=solovey-ui --property=Group=solovey-ui /usr/bin/sleep infinity
panel_uid=$(id -u solovey-ui)
panel_gid=$(id -g solovey-ui)
install -d -m 0755 /etc/solovey-ui /usr/local/lib/solovey-ui/systemd /usr/local/solovey-ui/releases/current /usr/local/solovey-ui/.runtime/server-protection /usr/local/solovey-ui/cert
install -d -m 0750 -o "$panel_uid" -g "$panel_gid" /var/lib/solovey-ui
install -d -m 0700 -o "$panel_uid" -g "$panel_gid" /var/lib/solovey-ui/db
install -d -m 0750 -o root -g "$panel_gid" /run/solovey-ui
install -d -m 0700 /var/lib/solovey-ui-broker
install -m 0755 "$build_root/api.test" /usr/local/solovey-ui/releases/current/solovey-ui
printf 'native-hardened\n' > /etc/solovey-ui/deployment-profile
for role in broker proof; do
 cp "deploy/systemd/solovey-privileged-$role.socket" "/run/systemd/system/solovey-privileged-$role.socket"
done
sed -e 's|^ExecStart=.*|ExecStart=/usr/local/solovey-ui/releases/current/solovey-ui -test.v -test.timeout=75s -test.run=^TestDeploymentSystemdPersistenceClient$|' \
 -e '/^\[Service\]/a Environment=SOLOVEY_TEST_DEPLOYMENT_CLIENT=1' \
 deploy/systemd/solovey-ui-native-hardened.service > /usr/local/lib/solovey-ui/systemd/solovey-ui-native-hardened.service
ln -s /usr/local/lib/solovey-ui/systemd/solovey-ui-native-hardened.service /etc/systemd/system/solovey-ui.service
install -d -m 0755 /etc/systemd/system/multi-user.target.wants
ln -s /etc/systemd/system/solovey-ui.service /etc/systemd/system/multi-user.target.wants/solovey-ui.service
{
 printf '[Unit]\nRequires=solovey-privileged-broker.socket solovey-privileged-proof.socket\n[Service]\nType=exec\nUser=root\nGroup=root\nEnvironment=SOLOVEY_TEST_DEPLOYMENT_BROKER=1\n'
 printf 'ExecStart=/usr/local/solovey-ui/releases/current/solovey-ui -test.v -test.timeout=95s -test.run=^TestDeploymentSystemdPersistenceBroker$\n'
 printf 'ReadWritePaths=/var/lib/solovey-ui-broker\n'
 while IFS= read -r line; do
  line=${line%$'\r'}
  case "$line" in
   NoNewPrivileges=*|PrivateTmp=*|PrivateDevices=*|Protect*=*|Restrict*=*|LockPersonality=*|MemoryDenyWriteExecute=*|SystemCallArchitectures=*|CapabilityBoundingSet=*|AmbientCapabilities=*|UMask=*) printf '%s\n' "$line";;
  esac
 done < deploy/systemd/solovey-privileged-broker.service
} > /run/systemd/system/solovey-privileged-broker.service
systemctl daemon-reload
since=$(date --iso-8601=seconds)
systemctl start solovey-ui.service
for ((attempt=0; attempt<80; attempt++)); do
 state=$(systemctl show solovey-ui.service -p ActiveState --value)
 [[ $state == active || $state == activating ]] || break
 sleep 1
done
journalctl -u solovey-ui.service -u solovey-privileged-broker.service --since "$since" --no-pager -o cat > "$build_root/runtime.log"
cat "$build_root/runtime.log"
[[ $state == inactive && $(systemctl show solovey-ui.service -p ExecMainStatus --value) == 0 ]]
grep -q 'REAL_SYSTEMD_DOCTOR_SQLITE_PUBLIC=PASS' "$build_root/runtime.log"
[[ $(grep -c 'DEPLOYMENT_AUDIT verb=deployment.doctor.observe phase=dispatch_gate result=success' "$build_root/runtime.log") == 6 ]]
echo 'REAL_NATIVE_PERSISTENCE=PASS; PHYSICAL_TARGET=NOT_TESTED'
