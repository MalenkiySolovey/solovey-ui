# Solovey UI 2026.3.2-beta.7

Deployment Status and Doctor persist observations using an atomic write-first
UPSERT, avoiding SQLite WAL read-to-write upgrade contention. Migration intent,
Doctor authority, busy timeout, connection concurrency and durability settings
retain their existing contracts.

Authenticated persistence diagnostics contain bounded stage, storage class and
SQLite codes. Paths, SQL, row contents and arbitrary underlying errors are
excluded. Required state writes fail closed; bounded Doctor retention preserves
the current report even when an older report is replayed.

Select this historical prerelease explicitly with
`--version v2026.3.2-beta.7`. Default installation resolves the current stable
release.
