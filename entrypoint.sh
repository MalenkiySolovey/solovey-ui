#!/bin/sh

MIGRATE_ONLY="${SOLOVEY_UI_MIGRATE_ONLY:-${SUI_MIGRATE_ONLY:-0}}"

RUNTIME_ROOT="${SUI_SERVER_PROTECTION_RUNTIME_ROOT:-}"
[ "$RUNTIME_ROOT" = "/run/solovey-ui/server-protection" ] || exit 1
[ -d /run/solovey-ui ] && [ ! -L /run/solovey-ui ] || exit 1
if [ -e "$RUNTIME_ROOT" ]; then
	[ -d "$RUNTIME_ROOT" ] && [ ! -L "$RUNTIME_ROOT" ] || exit 1
else
	mkdir -m 0700 "$RUNTIME_ROOT" || exit 1
fi
chmod 0700 "$RUNTIME_ROOT" || exit 1

if [ "${MIGRATE_ONLY}" = "1" ]; then
	exec ./solovey-ui migrate
fi

exec ./solovey-ui
