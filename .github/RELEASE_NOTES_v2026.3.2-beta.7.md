# Solovey UI 2026.3.2-beta.7

Deployment observations now persist with an atomic write-first UPSERT. A real
SQLite WAL regression reproduced beta.6's Status-to-Doctor failure at
SavePosture with SQLITE_BUSY_SNAPSHOT (extended code 517) after an independent
writer committed. Existing migration intent and Doctor authority are preserved;
WAL, busy timeout, connection concurrency and durability settings are unchanged.

Persistence failures retain a bounded semantic stage, storage class and SQLite
codes in the authenticated API and server diagnostics. SQL, paths, row contents
and arbitrary underlying error text are excluded. Required persistence remains
fail-closed. Doctor snapshots and revision updates remain transactional, and
64-row history retention preserves the current report even on an older replay.

Regression coverage includes the production SQLite bootstrap and manager,
concurrent writers, rollback and retention, real systemd socket activation through
the production broker/provider to the authenticated API, package-managed
OpenWrt persistence, and a real Docker provider gate. Broker credentials, pidfd,
cross-UID authority and the capability boundary are unchanged; CAP_KILL remains
absent.

The exact underlying error on the preserved physical beta.6 device was not
captured. The WAL mechanism is reproduction-backed, not a recovered historical
errno. NanoPi access and physical beta.6-to-beta.7 qualification are deferred to
a separately authorized run. Stable/latest remains v2026.3.1.
