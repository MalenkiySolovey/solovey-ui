# Contributing

Solovey UI is organized around a compact core and optional components. Keep new
feature code close to the owner that registers and removes it.

## Local Checks

Run the smallest check that covers your change, then run the broader checks
before publishing:

```sh
make audit:build
make audit:vet
make audit:test-go
make audit:fe-lint
make audit:fe-build
make audit:test-fe
```

Additional blocking checks:

```sh
make audit:test-go-race
make audit:lint-go
make audit:vuln
```

The blocking lint configuration is `.golangci-blocking.yml`. The broader lint
report and `make audit:gosec` retain advisory findings for review.

Run benchmarks separately when a change affects performance:

```sh
go test ./... -bench=. -benchmem -run=^$ -benchtime=2s
```

Linux process/socket contracts use `bash scripts/native-linux-contract.sh`.
Pinned OpenWrt capability checks use
`node scripts/openwrt-listener-capability-contract.mjs`. These source and host
checks protect platform contracts; supported hardware/storage combinations are
documented with their deployment owner.

Classic and Nexus share one source owner, `MalenkiySolovey/solovey-frontend`.
Edit frontend features there. Product admits the immutable producer identity in
`deploy/dependencies/solovey-frontend.json`; component source acceptance remains
in `components/sources.lock.json`. Run these commands from the Product root
with Node from `.node-version`:

```sh
node scripts/frontend.mjs install
node scripts/frontend.mjs test:unit
node scripts/frontend.mjs build
```

`build` verifies and consumes the selected qualified distribution. Audit, test
and development commands use an explicit hash-verified source fixture under
`.artifacts/frontend/`. Fixture source edits fail validation; change the canonical
repository and qualify a new immutable producer version instead. For offline
consumption, prepopulate the verified cache and set `SOLOVEY_FRONTEND_OFFLINE=1`.
A new profile or component input tuple requires its own producer qualification.

Playwright setup:

```sh
node scripts/frontend.mjs browser-install chromium
node scripts/frontend.mjs test:e2e
```

## Component Rules

- Core packages must not import `components/*`.
- Component behavior tests should live inside the component package.
- Core tests may use synthetic fixture components to verify generic host,
  routing, and lifecycle contracts.
- Disable means unregister runtime behavior while keeping data.
- Remove means remove component files and runtime registration.
- Data deletion must be explicit and handled by the component owner.

## Fixtures

Some migration tests require local database fixtures under `test-db/`. These
fixtures may contain private operational data and must stay out of the
repository.
