# S-UI Database Import Troubleshooting

This note documents the legacy S-UI database import behavior seen with the
local sample from `Разное`.

## What Is Expected

After importing an S-UI database, the browser can be redirected to the login
screen. That is expected because the session store and cookie signing data come
from the restored database, so the old browser session no longer matches the
new DB.

Use the admin credentials from the imported S-UI database. Legacy plaintext
passwords are rehashed automatically on first startup/import.

## Windows Local Testing

On Windows, SIGHUP is not available. Older code killed the process after a DB
import because the import path tried to emulate SIGHUP with `process.Kill()`.
Solovey UI now registers an in-process restart callback from `main.go`, so a DB
import restarts the panel without terminating the local process.

## Stale Listen Address

The sample DB stores `webListen` as a concrete LAN address. If that address does
not exist on the current machine, Solovey UI falls back to `:2095` and keeps the
panel reachable.

After login, update the listen address from the panel settings if you want to
remove the warning.

## Sing-Box Core Does Not Start

The sample DB contains a `tun` inbound tagged `tun-in` with `auto_redirect`
enabled. On Windows local testing this can fail with:

```text
initialize inbound[ 1 ] tun-in initialize auto-redirect: invalid argument
```

The web panel should stay up so the configuration can be edited. For local UI
testing, disable or remove the `tun-in` inbound. For Linux server testing, check
that the host supports the selected TUN options and that the panel has the
required privileges/capabilities.

