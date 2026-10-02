#!/usr/bin/env bash
# Real host systemd start-limit recovery and cold socket activation. All units
# and data are uniquely named fixtures; no installed application is touched.
set -Eeuo pipefail
[[ $EUID == 0 ]] || exec sudo -n bash "$0"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d /tmp/solovey-activation.XXXXXXXX)"
PREFIX="solovey-activation-$$"
PANEL="${PREFIX}.service"
BROKER="${PREFIX}-broker.service"
SOCKET="${PREFIX}-broker.socket"
PROOF="${PREFIX}-proof.socket"
UNITS=("$SOCKET" "$PROOF" "$BROKER" "$PANEL")
cleanup() {
    systemctl stop "${UNITS[@]}" >/dev/null 2>&1 || true
    systemctl disable "${UNITS[@]}" >/dev/null 2>&1 || true
    for unit in "${UNITS[@]}"; do rm -f "/run/systemd/system/$unit"; done
    systemctl daemon-reload
    rm -rf "$TMP"
}
trap cleanup EXIT
for unit in "$SOCKET" "$PROOF"; do
    cat > "/run/systemd/system/$unit" <<EOF
[Socket]
ListenStream=$TMP/$unit.sock
Service=$BROKER
[Install]
WantedBy=sockets.target
EOF
done
cat > "/run/systemd/system/$BROKER" <<EOF
[Unit]
Requires=$SOCKET $PROOF
After=$SOCKET $PROOF
[Service]
ExecStart=/bin/sleep infinity
EOF
cat > "/run/systemd/system/$PANEL" <<EOF
[Unit]
Requires=$SOCKET $PROOF
After=$SOCKET $PROOF
[Service]
Type=exec
ExecStart=/bin/sh -c 'mkdir -p $TMP/data/db; touch $TMP/data/db/panel.db; exec /bin/sleep infinity'
[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
# A successful replacement must clear the old release's start limit without
# prewarming its broker. Exercise the installer's production activation owner.
systemctl stop "${UNITS[@]}"
cat > "/run/systemd/system/$BROKER" <<EOF
[Unit]
Requires=$SOCKET $PROOF
After=$SOCKET $PROOF
StartLimitIntervalSec=1h
StartLimitBurst=1
[Service]
ExecStart=/bin/false
EOF
systemctl daemon-reload
systemctl start "$SOCKET" "$PROOF"
systemctl start "$BROKER" || true
for attempt in {1..50}; do
    [[ $(systemctl show "$BROKER" -p ActiveState --value) != failed ]] || break
    sleep .02
done
[[ $(systemctl show "$BROKER" -p ActiveState --value) == failed ]]
sed -i 's@ExecStart=/bin/false@ExecStart=/bin/sleep infinity@' "/run/systemd/system/$BROKER"
systemctl daemon-reload
if systemctl start "$BROKER"; then echo 'old start limit was not reproduced'; exit 1; fi
[[ $(systemctl show "$BROKER" -p MainPID --value) == 0 ]]
systemctl stop "$SOCKET" "$PROOF"
(
    source "$ROOT/install.sh"
    SERVICE_NAME="${PANEL%.service}"
    BROKER_UNITS=("$BROKER" "$SOCKET" "$PROOF")
    activate_install_runtime
)
[[ $(systemctl show "$BROKER" -p MainPID --value) == 0 ]]
[[ $(systemctl show "$SOCKET" -p SubState --value) == listening ]]
[[ $(systemctl show "$PROOF" -p SubState --value) == listening ]]
python3 - "$TMP/$SOCKET.sock" <<'PY'
import socket,sys
with socket.socket(socket.AF_UNIX) as client:
    client.connect(sys.argv[1])
PY
for attempt in {1..50}; do
    [[ $(systemctl show "$BROKER" -p MainPID --value) == 0 ]] || break
    sleep .02
done
[[ $(systemctl show "$BROKER" -p MainPID --value) != 0 ]]
echo 'PASS: replacement clears old start limit; first socket request activates broker'
