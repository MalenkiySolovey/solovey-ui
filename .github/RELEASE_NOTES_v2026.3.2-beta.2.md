# Solovey UI v2026.3.2-beta.2

This prerelease improves native Linux installation and failure recovery.
Application-owner validation consumes the correct filesystem metadata and
publishes exact owner/group/mode before rename. Failed installations restore
prior database presence, service/socket activity and systemd enablement from a
quiescent snapshot. Incomplete rollback retains recovery material and prevents
an unsafe retry.

Native-hardened and native-legacy-root use the same contract. Full/core
composition and component selection retain their boundaries. The SSH reconnect
proof helper retains root ownership, the dedicated nonroot group and mode 2755.

Select this historical prerelease explicitly with
`--version v2026.3.2-beta.2`. Default installation resolves the current stable
release.
