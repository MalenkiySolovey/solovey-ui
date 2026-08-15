# Windows Helpers

This directory contains Windows service and build helpers for Solovey UI.

## Files

- `s-ui-windows.xml`: Windows service configuration.
- `install-windows.bat`: service installation helper.
- `configure-windows.ps1`: validated interactive panel configuration helper.
- `s-ui-windows.bat`: local control helper.
- `control-windows.ps1`: implementation of the safe local control menu.
- `uninstall-windows.bat`: service removal helper.
- `uninstall-windows.ps1`: validated service and file removal helper.
- `build-windows.bat`: CMD build helper.
- `build-windows.ps1`: PowerShell build helper.

Run installation helpers from an elevated terminal. Windows release packages
are supported on x64 only and use the CGO-backed SQLite runtime. ARM64 is not
shipped because the current SQLite provider has no functional non-CGO runtime.
