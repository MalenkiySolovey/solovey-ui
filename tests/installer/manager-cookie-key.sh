#!/usr/bin/env bash

set -Eeuo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

FAKEBIN="${TMP}/fakebin"
ENV_DIR="${TMP}/etc/solovey-ui"
LOG="${TMP}/systemctl.log"
mkdir -p "${FAKEBIN}" "${ENV_DIR}"

cat > "${FAKEBIN}/systemctl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${TEST_SYSTEMCTL_LOG}"
SH
chmod +x "${FAKEBIN}/systemctl"

printf 'SUI_SECRETBOX_KEY=stable\nSUI_COOKIE_KEY=old-cookie\n' > "${ENV_DIR}/secretbox.env"

PATH="${FAKEBIN}:${PATH}" \
TEST_SYSTEMCTL_LOG="${LOG}" \
SOLOVEY_UI_ALLOW_NON_ROOT=1 \
SOLOVEY_UI_ENV_DIR="${ENV_DIR}" \
bash "${ROOT}/solovey-ui.sh" rotate-cookie-key

grep -Eq '^SUI_SECRETBOX_KEY=stable$' "${ENV_DIR}/secretbox.env"
grep -Eq '^SUI_COOKIE_KEY=.{40,}$' "${ENV_DIR}/secretbox.env"
! grep -Eq '^SUI_COOKIE_KEY=old-cookie$' "${ENV_DIR}/secretbox.env"
grep -Eq '^restart solovey-ui$' "${LOG}"
find "${ENV_DIR}" -maxdepth 1 -name 'secretbox.env.bak.*' -type f | grep -q .

printf 'PASS: manager cookie-key rotation integration\n'
