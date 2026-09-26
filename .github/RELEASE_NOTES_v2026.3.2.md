# Solovey UI 2026.3.2

This patch fixes the native Linux installer stopping at broker manifest creation
after installing the SSH reconnect proof helper. The helper remains owned by
root with the dedicated `solovey-ui` group and setgid mode 2755. Manifest creation,
runtime peer authentication and native updates now validate that exact contract
without granting root group identity or relaxing other executable policies.

CI and release qualification execute the real installer ownership operation and
manifest writer, with unsafe ownership/mode cases and a real non-root setgid
socket regression. Full and core native update paths preserve the same contract.

This release also includes the accepted post-2026.3.1 persistence-policy build
isolation, shared capability-aware qualification and synchronous completion of
restore and session-rotation audit records before database handoff.

The Debian 13/Armbian physical failure on 2026.3.1 is recorded. Physical recovery
and clean-install qualification of this patch are separate follow-up gates; this
release does not claim those physical tests have already passed. Existing
OpenWrt/FriendlyWrt ownership and deployment contracts are preserved.

