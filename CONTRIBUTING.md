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

Frontend setup:

```sh
cd frontend
npm ci
npm run test:unit
npm run build
```

Playwright setup:

```sh
cd frontend
npx playwright install chromium
npx playwright test
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
