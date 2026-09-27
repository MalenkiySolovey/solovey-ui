#!/usr/bin/env bash
# Real host systemd, real files/links and real EXIT/signal rollback. All units
# and data are uniquely named fixtures; no installed application is touched.
set -Eeuo pipefail
[[ $EUID == 0 ]] || exec sudo -n bash "$0"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d /tmp/solovey-transaction.XXXXXXXX)"
PREFIX="solovey-transaction-$$"
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
for activity in inactive active; do
    for enablement in disabled enabled-runtime enabled; do
        for failure in before-migration after-migration signal absent-data; do
            [[ $failure != absent-data || $activity == inactive ]] || continue
            systemctl stop "${UNITS[@]}"
            systemctl disable --runtime "$PANEL" "$SOCKET" "$PROOF" >/dev/null 2>&1
            systemctl disable "$PANEL" "$SOCKET" "$PROOF" >/dev/null 2>&1
            if [[ $enablement == enabled-runtime ]]; then systemctl enable --runtime "$PANEL" "$SOCKET" "$PROOF" >/dev/null 2>&1; fi
            if [[ $enablement == enabled ]]; then systemctl enable "$PANEL" "$SOCKET" "$PROOF" >/dev/null 2>&1; fi
            rm -rf "$TMP/app" "$TMP/etc" "$TMP/data" "$TMP/backups"
            mkdir -p "$TMP/app" "$TMP/etc" "$TMP/data/db"
            [[ $failure != absent-data ]] || rm -rf "$TMP/data"
            printf 'old release\n' > "$TMP/app/BUILD_INFO.txt"
            cp /bin/true "$TMP/app/solovey-ui"
            before_inode="$(stat -c '%d:%i' "$TMP/app/solovey-ui")"
            ln -s "$TMP/app/manager" "$TMP/cli" 2>/dev/null || true
            if [[ $activity == active ]]; then
                systemctl start "$PANEL" "$BROKER"
                for attempt in {1..50}; do [[ ! -f "$TMP/data/db/panel.db" ]] || break; sleep .02; done
                [[ -f "$TMP/data/db/panel.db" ]]
            fi
            set +e
            bash -c '
                source "$1/install.sh"
                SERVICE_NAME="$3"; SERVICE_NAME="${SERVICE_NAME%.service}"
                SYSTEMD_SERVICE="/run/systemd/system/$3"
                SYSTEMD_UNIT_ROOT=/etc/systemd/system; SYSTEMD_RUNTIME_UNIT_ROOT=/run/systemd/system
                SYSTEMD_PROFILE_ROOT="$2/profiles"
                INSTALL_DIR="$2/app"; ENV_DIR="$2/etc"; HARDENED_DATA_ROOT="$2/data"
                CLI_PATH="$2/cli"; BACKUP_ROOT="$2/backups"
                TRANSACTION_UNITS=("$4" "$5" "$6" "$3")
                BROKER_UNITS=("$6" "$4" "$5")
                trap '\''finish_install_transaction "$?"'\'' EXIT
                trap '\''exit 143'\'' TERM
                begin_install_transaction
                if bash -c '\''source "$1/install.sh"; BACKUP_ROOT="$2/backups"; begin_install_transaction'\'' fence "$1" "$2" > "$2/fence-result" 2>&1; then exit 99; fi
                grep -q "unfinished or concurrent native installation" "$2/fence-result"
                printf "new release\n" > "$INSTALL_DIR/BUILD_INFO.txt"
                mkdir -p "$HARDENED_DATA_ROOT/db"
                systemctl enable --runtime "$3" "$4" "$5"
                if [[ "$7" == after-migration ]]; then
                    printf "new DB\n" > "$HARDENED_DATA_ROOT/db/panel.db"
                    systemctl start "$3"
                fi
                if [[ "$7" == signal ]]; then kill -TERM $$; fi
                exit 42
            ' transaction "$ROOT" "$TMP" "$PANEL" "$SOCKET" "$PROOF" "$BROKER" "$failure" > "$TMP/result" 2>&1
            result=$?
            set -e
            if [[ $result == 0 ]] || ! grep -q 'rollback after failed install completed' "$TMP/result"; then cat "$TMP/result"; exit 1; fi
            [[ $(systemctl show "$PANEL" -p ActiveState --value) == "$activity" ]]
            [[ $(systemctl is-enabled "$PANEL" || true) == "$enablement" ]]
            for unit in "$SOCKET" "$PROOF"; do
                [[ $(systemctl show "$unit" -p ActiveState --value) == "$activity" ]]
                [[ $(systemctl is-enabled "$unit" || true) == "$enablement" ]]
            done
            [[ $(cat "$TMP/app/BUILD_INFO.txt") == 'old release' ]]
            [[ $(stat -c '%d:%i' "$TMP/app/solovey-ui") == "$before_inode" ]]
            if [[ $activity == inactive ]]; then [[ ! -e "$TMP/data/db/panel.db" ]]; else [[ -f "$TMP/data/db/panel.db" ]]; fi
            [[ $failure != absent-data || ! -e "$TMP/data" ]]
            [[ ! -e "$TMP/backups/.install-transaction" ]]
            printf 'PASS: native rollback %s %s %s\n' "$activity" "$enablement" "$failure"
        done
    done
done
