#!/bin/sh

MIGRATE_ONLY="${SOLOVEY_UI_MIGRATE_ONLY:-${SUI_MIGRATE_ONLY:-0}}"

if [ "${MIGRATE_ONLY}" = "1" ]; then
	exec ./solovey-ui migrate
fi

exec ./solovey-ui
