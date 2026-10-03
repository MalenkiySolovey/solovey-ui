# Solovey UI 2026.3.2-beta.6

This prerelease fixes cold systemd socket activation for broker-backed
Deployment, SSH and Update operations. Linux clients accept the root PID1 peer
of a systemd-created Unix listener while retaining fixed socket paths, root
ownership, per-request credentials, connector pidfd, executable, cgroup and
role checks. CAP_KILL remains absent.

Receive failures expose bounded diagnostic classes without request contents.
Rejected truncated descriptor transfers close every descriptor delivered by
the kernel. Framing and Windows transport retain their existing contracts.

Select this historical prerelease explicitly with
`--version v2026.3.2-beta.6`. Default installation resolves the current stable
release.
