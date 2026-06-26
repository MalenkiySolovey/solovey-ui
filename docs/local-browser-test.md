# Local Browser Test

Use this workflow when you want to touch the panel in a browser without
polluting the repository with test data.

All local runtime files are kept under `.runtime/local-panel/`, which is ignored
by git. The helper also uses ignored build output locations: `bin/` for the Go
binary and `web/html` for the embedded frontend files.

When this repository is used inside the current analysis workspace, the helper
also auto-detects local `.devtools` Go, Node.js, and Zig toolchains. In a fresh
GitHub clone, install Go and Node.js normally or provide your own compatible
toolchain in `PATH`.

## Start From A Clean Runtime

Run from the repository root:

```powershell
cd path\to\solovey-ui
.\scripts\dev\start-panel.cmd -Fresh -OpenBrowser
type .runtime\local-panel\startup-summary.txt
```

The helper will:

- create `.runtime/local-panel/db` for the test SQLite database;
- generate `.runtime/local-panel/secretbox.env` for encrypted settings;
- build `web/html` from `frontend/dist` when it is missing or `-Build` is used;
- build `bin/solovey-ui.exe` when it is missing;
- start the panel on `http://127.0.0.1:2095/app/`;
- print the initial admin password when the database is created for the first
  time.

The startup summary is also written to `.runtime/local-panel/startup-summary.txt`
so the URL, PID, log paths, and initial admin password are still available if a
terminal does not show the helper output.

If you do not want to open the browser automatically, omit `-OpenBrowser`:

```powershell
.\scripts\dev\start-panel.cmd -Fresh
type .runtime\local-panel\startup-summary.txt
```

## Reuse The Same Local Runtime

For repeated manual checks where you want to keep the local test database:

```powershell
.\scripts\dev\start-panel.cmd -OpenBrowser
type .runtime\local-panel\startup-summary.txt
```

## Rebuild Frontend And Backend

Force fresh frontend assets and a fresh backend binary:

```powershell
.\scripts\dev\start-panel.cmd -Build -OpenBrowser
type .runtime\local-panel\startup-summary.txt
```

## Stop And Clean

Stop the local panel but keep the runtime database:

```powershell
.\scripts\dev\stop-panel.cmd
```

Stop the local panel and remove all local test data:

```powershell
.\scripts\dev\stop-panel.cmd -Clean
```

## Confirm Repository Cleanliness

After manual testing:

```powershell
git status --short
git check-ignore -v .runtime/local-panel/db/solovey-ui.db .runtime/local-panel/logs/panel.out.log bin/solovey-ui.exe web/html/index.html
```

The first command should show only intentional source changes. The second
command should prove that browser-test artifacts are ignored.
