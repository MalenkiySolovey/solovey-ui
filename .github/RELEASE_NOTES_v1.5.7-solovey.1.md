# Solovey UI v1.5.7-solovey.1

Initial personal release of Solovey UI.

## Highlights

- Renamed runtime, service, install paths, release artifacts, and local helpers to `solovey-ui`.
- Added Linux install/update flow based on GitHub Release artifacts.
- Added local backup, rollback, doctor, and uninstall commands through `solovey-ui`.
- Added optional migration path from legacy `/usr/local/s-ui`.
- Added local Windows browser-test helpers with ignored runtime data.
- Cleaned and modularized core service/settings save paths for easier maintenance.
- Added Solovey UI branding and a read-only sing-box config viewer in the panel.
- Made DNS rule list inputs consistent with routing rules by using one entry per line.

## Notes

This project is maintained for personal use and is provided without warranty.
Test on a non-production server before using it on a real machine.
