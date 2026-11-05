# Windows Helpers

This directory contains Windows service and build helpers for Solovey UI.

## Files

- `s-ui-windows.xml`: Windows service configuration.
- `install-windows.bat`: service installation helper.
- `configure-windows.ps1`: validated interactive panel configuration helper.
- `prepare-windows-install.ps1`: verifies and installs the native companion and protects private state permissions.
- `s-ui-windows.bat`: local control helper.
- `control-windows.ps1`: implementation of the safe local control menu.
- `uninstall-windows.bat`: service removal helper.
- `uninstall-windows.ps1`: validated service and file removal helper.
- `build-windows.bat`: CMD build helper.
- `build-windows.ps1`: PowerShell build helper.

Run installation helpers from an elevated terminal. Windows release packages
are supported on x64 and ARM64 and use the CGO-backed SQLite runtime. Release
builds run natively on their target architecture so SQLite persistence is
functional in both packages.

Both Full/Core profiles support the explicit `sing-box` (default) and
`shtorm-extended` build flavors through the same producer. For a production
candidate, supply `-FrontendDist`, `-TrustRootsFile` and a new
`-PackageDirectory`. This reuses verified frontend output, records the clean
source identity, selects the pinned module graph and includes the authenticated
architecture-specific `libcronet.dll`. The release workflow calls this producer
and seals its ZIP; the verifier inspects the executable and DLL without running
them. `-CrossCompile` requires an explicit CGO compiler and provides build proof
only; native target tests remain required. No release publication is performed
by the producer.

Installation copies the pinned DLL matching the panel's PE architecture before
migration or service startup. Database, certificate and log files are accessible
to LocalSystem, Administrators and the installing user. This also supports the
documented manual ARM64 mode without granting other local users access to secrets.
