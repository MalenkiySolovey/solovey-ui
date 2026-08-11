#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fixture_root="$(mktemp -d)"
trap 'rm -rf "${fixture_root}"' EXIT

command -v systemd-analyze >/dev/null

install -d \
  "${fixture_root}/etc/systemd/system" \
  "${fixture_root}/etc/solovey-ui" \
  "${fixture_root}/etc/ssh/sshd_config.d" \
  "${fixture_root}/usr/local/lib/solovey-ui/systemd" \
  "${fixture_root}/usr/local/solovey-ui/.runtime/server-protection" \
  "${fixture_root}/usr/local/solovey-ui/cert" \
  "${fixture_root}/usr/local/solovey-ui/releases/current" \
  "${fixture_root}/var/lib/solovey-ui/db" \
  "${fixture_root}/var/lib/solovey-ui-broker" \
  "${fixture_root}/run/systemd" \
  "${fixture_root}/bin"

for profile in native-hardened native-network-advanced native-legacy-root; do
  install -m 0644 \
    "${repository_root}/deploy/systemd/solovey-ui-${profile}.service" \
    "${fixture_root}/usr/local/lib/solovey-ui/systemd/solovey-ui-${profile}.service"
  ln -s "../../../usr/local/lib/solovey-ui/systemd/solovey-ui-${profile}.service" \
    "${fixture_root}/etc/systemd/system/solovey-ui-${profile}.service"
done
for unit in solovey-privileged-broker.service solovey-privileged-broker.socket solovey-privileged-proof.socket; do
  install -m 0644 \
    "${repository_root}/deploy/systemd/${unit}" \
    "${fixture_root}/etc/systemd/system/${unit}"
done
ln -s solovey-ui-native-hardened.service \
  "${fixture_root}/etc/systemd/system/solovey-ui.service"

install -m 0755 /bin/true "${fixture_root}/usr/local/solovey-ui/releases/current/solovey-ui"
install -m 0755 /bin/true "${fixture_root}/usr/local/solovey-ui/releases/current/solovey-privileged-broker"
install -m 0755 /bin/true "${fixture_root}/bin/kill"

# recursive-errors=no still fails for every problem in the explicitly supplied
# unit while ignoring unrelated host dependency warnings in this offline root.
systemd-analyze verify --root="${fixture_root}" --man=no --recursive-errors=no \
  /etc/systemd/system/solovey-ui.service \
  /etc/systemd/system/solovey-ui-native-network-advanced.service \
  /etc/systemd/system/solovey-ui-native-legacy-root.service \
  /etc/systemd/system/solovey-privileged-broker.service \
  /etc/systemd/system/solovey-privileged-broker.socket \
  /etc/systemd/system/solovey-privileged-proof.socket

systemd-analyze security --offline=yes --root="${fixture_root}" \
  /etc/systemd/system/solovey-ui.service \
  /etc/systemd/system/solovey-privileged-broker.service >/dev/null
