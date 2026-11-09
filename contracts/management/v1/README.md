# Panel management/v1 compatibility boundary

The sole editable schema, codec, generator and conformance vectors are maintained
in [solovey-contracts](https://github.com/MalenkiySolovey/solovey-contracts).
Panel and Node use the same accepted immutable Go module and checksum recorded
in `contracts/PIN.json`. Product stable remains `v2026.5.1`; schema remains 1.13.

Panel's production/test imports use
`github.com/MalenkiySolovey/solovey-contracts/management/v1` directly. The old
published Panel package path remains as `compat.go`, a bounded generated type
alias/function forwarding shim for existing source importers. It owns no schema,
validation algorithm or catalog. It is deprecated; keep it until an explicitly
reviewed public-import retirement, rather than breaking existing importers during
this extraction. No runtime owner or authorization decision is moved.

`vectors.json` and `manifest.json` are read-only fixtures derived from the actual
resolved module. They support existing owner-adapter, StepUp, Restore and
supervisor tests without introducing network access inside those tests.

With Go 1.26.10, Node 24.16.0 and `GOWORK=off`:

```text
node scripts/verify-contract-pin.mjs
node --test scripts/verify-contract-pin.test.mjs
```

The verifier checks both Official and Extended modfiles, real immutable module
resolution/checksums, source provenance, canonical bundle/generator hashes,
fixtures, generated shim drift and the absence of an editable schema or old
Panel-to-Node copy writer. It rejects workspace, replace and GOFLAGS source
substitution. An intentional accepted pin update can refresh only these derived
fixtures and the shim with `--sync-fixtures`; no schema is copied back.

Codec source tests now run in Contracts CI and the Panel conformance job also
executes the pinned external codec suite. Runtime permissions, one-use grants,
owner revisions, Change/Job/Health, Backup/Restore and signed updates retain
their existing Panel owners. Source rollback reverts the consumer pin/imports,
derived adapter and old-authority retirement together; no data/schema downgrade,
release-tag movement or module-tag mutation is required.
