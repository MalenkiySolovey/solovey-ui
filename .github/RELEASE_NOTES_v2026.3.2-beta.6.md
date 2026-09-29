# Solovey UI 2026.3.2-beta.6

This candidate repairs Linux broker clients rejecting root PID1 as the peer of
a systemd-created Unix listener. The rejection closed the connection before
WriteFrame; the broker passed initial attestation and then rejected an empty
receive. A real systemd cross-UID production client/server regression reproduced
that failure against beta.5 and now completes the capability exchange.

The fixed socket path, root ownership, per-request SCM_CREDENTIALS, connector
pidfd, executable, cgroup, role and final recheck policies remain enforced.
CAP_KILL remains absent. Stream framing and Windows transport are unchanged.
Receive failures now retain bounded internal semantic diagnostics without
request contents or arbitrary error strings. Truncated SCM_RIGHTS rejection
also closes the descriptors delivered by the kernel.

CI requires three cold systemd socket activations and 24 nonroot requests under
the production broker hardening/capability policy. Focused regressions cover
queued/fragmented frames, writer changes, inherited/transferred sockets, exec
changes, dead connectors and descriptor rejection.

The preserved Debian13/Armbian beta.5 fixture has not been accessed for this
source release. Physical beta.5-to-beta.6 upgrade, stability/reboot and subsequent
clean-install acceptance remain separate required propositions. This prerelease
does not claim physical acceptance. Stable/latest remains v2026.3.1.
